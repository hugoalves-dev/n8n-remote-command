package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	maxMessageBytes  = 8 * 1024
	reconnectMaxWait = time.Minute

	// Deduplication keeps a commandId for longer than any action may stay valid.
	dedupWindow     = time.Hour
	dedupMaxEntries = 4096

	resultRetention     = 500
	auditRetention      = 1000
	deadLetterRetention = 200
)

type config struct {
	MachineID string `json:"machineId"`
	SSH       struct {
		Host       string `json:"host"`
		Port       int    `json:"port"`
		Username   string `json:"username"`
		ClientKey  string `json:"clientKey"`
		KnownHosts string `json:"knownHosts"`
	} `json:"ssh"`
	Redis struct {
		RemoteHost string `json:"remoteHost"`
		RemotePort int    `json:"remotePort"`
		Database   int    `json:"database"`
	} `json:"redis"`
	LogFile               string   `json:"logFile"`
	AnyDeskExecutablePath string   `json:"anyDeskExecutablePath"`
	AllowedActions        []string `json:"allowedActions"`
	enabled               actionSet
}

type command struct {
	Version       int             `json:"version"`
	CommandID     string          `json:"commandId"`
	CorrelationID string          `json:"correlationId"`
	Target        string          `json:"target"`
	Action        string          `json:"action"`
	Args          json.RawMessage `json:"args"`
	RequestedBy   string          `json:"requestedBy"`
	RequestedAt   time.Time       `json:"requestedAt"`
	ExpiresAt     time.Time       `json:"expiresAt"`
	arguments     any
}

type outcome struct {
	Matched         int  `json:"matched"`
	ForceKilled     int  `json:"forceKilled"`
	ReopenAttempted bool `json:"reopenAttempted"`
	Reopened        bool `json:"reopened"`
}

type commandRunner func(string, ...string) (string, error)
type anyDeskReopener func(string) bool

func loadConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, err
	}
	var c config
	if err := json.Unmarshal(data, &c); err != nil {
		return config{}, err
	}
	if c.MachineID == "" || c.SSH.Host == "" || c.SSH.Username == "" {
		return config{}, errors.New("machineId and SSH host/username are required")
	}
	if c.SSH.Port == 0 {
		c.SSH.Port = 22
	}
	if c.Redis.RemoteHost == "" {
		c.Redis.RemoteHost = "127.0.0.1"
	}
	if c.Redis.RemotePort == 0 {
		c.Redis.RemotePort = 6379
	}
	if c.Redis.Database < 0 {
		return config{}, errors.New("Redis database must be zero or greater")
	}
	if c.LogFile == "" {
		c.LogFile = "logs/anydesk-agent.log"
	}
	if _, err := os.Stat(c.SSH.ClientKey); err != nil {
		return config{}, fmt.Errorf("SSH clientKey: %w", err)
	}
	if _, err := os.Stat(c.SSH.KnownHosts); err != nil {
		return config{}, fmt.Errorf("SSH knownHosts: %w", err)
	}
	c.enabled = defaultActions()
	if c.AllowedActions != nil {
		enabled, err := configuredActions(c.AllowedActions)
		if err != nil {
			return config{}, err
		}
		c.enabled = enabled
	}
	return c, nil
}

func validateCommand(raw, machineID string, allowed actionSet, now time.Time) (command, actionSpec, error) {
	if len([]byte(raw)) >= maxMessageBytes {
		return command{}, actionSpec{}, errors.New("message is too large")
	}
	var c command
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return command{}, actionSpec{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if c.Version != 1 {
		return command{}, actionSpec{}, errors.New("unsupported version")
	}
	if c.CommandID == "" || c.CorrelationID == "" {
		return command{}, actionSpec{}, errors.New("commandId and correlationId are required")
	}
	if c.Target != machineID {
		return command{}, actionSpec{}, errors.New("message target does not match this machine")
	}
	if len(c.RequestedBy) > maxRequesterLength {
		return command{}, actionSpec{}, errors.New("requestedBy is too long")
	}
	if _, known := catalog[c.Action]; !known {
		return command{}, actionSpec{}, errors.New("action is not allowed")
	}
	spec, authorized := allowed[c.Action]
	if !authorized {
		return command{}, actionSpec{}, errors.New("action is not authorized on this machine")
	}
	arguments, err := spec.parseArgs(c.Args)
	if err != nil {
		return command{}, actionSpec{}, err
	}
	c.arguments = arguments
	if c.RequestedAt.Location() != time.UTC || c.ExpiresAt.Location() != time.UTC {
		return command{}, actionSpec{}, errors.New("timestamps must be UTC")
	}
	if !c.ExpiresAt.After(c.RequestedAt) || !c.ExpiresAt.After(now) {
		return command{}, actionSpec{}, errors.New("message has expired")
	}
	if c.ExpiresAt.Sub(c.RequestedAt) > spec.maxLifetime {
		return command{}, actionSpec{}, fmt.Errorf("command lifetime exceeds the %s maximum for this action", spec.maxLifetime)
	}
	return c, spec, nil
}

func killAnyDesk(run commandRunner, tasklistOutput string) (outcome, error) {
	matched := 0
	for _, line := range strings.Split(tasklistOutput, "\n") {
		if strings.Contains(strings.ToLower(line), "anydesk.exe") {
			matched++
		}
	}
	if matched == 0 {
		return outcome{}, nil
	}
	_, err := run("taskkill", "/IM", "AnyDesk.exe", "/F")
	if err != nil {
		return outcome{}, fmt.Errorf("taskkill AnyDesk.exe: %w", err)
	}
	return outcome{Matched: matched, ForceKilled: matched}, nil
}

func anyDeskExecutableCandidates(customPath string) []string {
	candidates := make([]string, 0, 4)
	if customPath != "" {
		candidates = append(candidates, customPath)
	}
	return append(candidates,
		filepath.Join(os.Getenv("ProgramFiles"), "AnyDesk", "AnyDesk.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "AnyDesk", "AnyDesk.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "AnyDesk", "AnyDesk.exe"),
	)
}

func reopenAnyDesk(customPath string) bool {
	candidates := anyDeskExecutableCandidates(customPath)
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return exec.Command(candidate).Start() == nil
		}
	}
	return false
}

func applyReopen(result outcome, reopenRequested bool, customPath string, reopen anyDeskReopener) outcome {
	result.ReopenAttempted = reopenRequested
	if reopenRequested {
		result.Reopened = reopen(customPath)
	}
	return result
}

func executeKill() (outcome, error) {
	list := exec.Command("tasklist", "/FI", "IMAGENAME eq AnyDesk.exe", "/FO", "CSV", "/NH")
	output, err := list.Output()
	if err != nil {
		return outcome{}, fmt.Errorf("list AnyDesk.exe: %w", err)
	}
	return killAnyDesk(func(name string, args ...string) (string, error) {
		output, err := exec.Command(name, args...).CombinedOutput()
		return string(output), err
	}, string(output))
}

func tunnel(client *ssh.Client, remote string) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	var once sync.Once
	closeAll := func() { once.Do(func() { _ = listener.Close() }) }
	go func() {
		for {
			local, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer local.Close()
				remoteConn, err := client.Dial("tcp", remote)
				if err != nil {
					return
				}
				defer remoteConn.Close()
				go io.Copy(remoteConn, local)
				_, _ = io.Copy(local, remoteConn)
			}()
		}
	}()
	return listener.Addr().String(), closeAll, nil
}

func sshClient(c config) (*ssh.Client, error) {
	key, err := os.ReadFile(c.SSH.ClientKey)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}
	hostKeyCallback, err := knownhosts.New(c.SSH.KnownHosts)
	if err != nil {
		return nil, err
	}
	return ssh.Dial("tcp", net.JoinHostPort(c.SSH.Host, fmt.Sprint(c.SSH.Port)), &ssh.ClientConfig{
		User: c.SSH.Username, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: hostKeyCallback,
		Timeout: 20 * time.Second,
	})
}

func redisOptions(c config, address string) *redis.Options {
	return &redis.Options{Addr: address, DB: c.Redis.Database}
}

func queue(machineID, suffix string) string { return "remote-agent:" + suffix + ":" + machineID }

func resultPayload(machineID string, c command, status, reason string, result any, completedAt time.Time) map[string]any {
	payload := map[string]any{
		"commandId":     c.CommandID,
		"correlationId": c.CorrelationID,
		"target":        machineID,
		"action":        c.Action,
		"status":        status,
		"completedAt":   completedAt,
	}
	if result != nil {
		payload["outcome"] = result
	}
	if reason != "" {
		payload["reason"] = boundedReason(reason)
	}
	return payload
}

func deadLetterPayload(machineID, raw, reason string, receivedAt time.Time) map[string]any {
	return map[string]any{
		"target":     machineID,
		"status":     auditRejected,
		"reason":     reason,
		"receivedAt": receivedAt,
		"raw":        raw,
	}
}

// publish appends one JSON entry and trims the list, so retention stays bounded.
func publish(ctx context.Context, client *redis.Client, key string, entry any, retention int64) error {
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := client.LPush(ctx, key, encoded).Err(); err != nil {
		return err
	}
	return client.LTrim(ctx, key, 0, retention-1).Err()
}

func report(ctx context.Context, client *redis.Client, machineID string, cmd command, status, reason string, result any, receivedAt, completedAt time.Time) error {
	if err := publish(ctx, client, queue(machineID, "results"), resultPayload(machineID, cmd, status, reason, result, completedAt), resultRetention); err != nil {
		return err
	}
	return publish(ctx, client, queue(machineID, "audit"), commandAudit(cmd, status, reason, receivedAt, completedAt), auditRetention)
}

func reportRejection(ctx context.Context, client *redis.Client, machineID, raw, reason string, receivedAt time.Time) error {
	if err := publish(ctx, client, queue(machineID, "dead-letter"), deadLetterPayload(machineID, raw, reason, receivedAt), deadLetterRetention); err != nil {
		return err
	}
	return publish(ctx, client, queue(machineID, "audit"), rejectionAudit(machineID, reason, receivedAt), auditRetention)
}

func consume(ctx context.Context, c config, ledger *commandLedger, logger *log.Logger) error {
	client, err := sshClient(c)
	if err != nil {
		return err
	}
	defer client.Close()
	address, closeTunnel, err := tunnel(client, net.JoinHostPort(c.Redis.RemoteHost, fmt.Sprint(c.Redis.RemotePort)))
	if err != nil {
		return err
	}
	defer closeTunnel()
	redisClient := redis.NewClient(redisOptions(c, address))
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return err
	}
	logger.Printf("connected to Redis database %d; waiting on %s", c.Redis.Database, queue(c.MachineID, "commands"))
	for ctx.Err() == nil {
		item, err := redisClient.BRPop(ctx, 30*time.Second, queue(c.MachineID, "commands")).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return err
		}
		raw := item[1]
		receivedAt := time.Now().UTC()
		cmd, spec, err := validateCommand(raw, c.MachineID, c.enabled, receivedAt)
		if err != nil {
			if reportErr := reportRejection(ctx, redisClient, c.MachineID, raw, err.Error(), receivedAt); reportErr != nil {
				return reportErr
			}
			logger.Printf("command rejected: %v", err)
			continue
		}
		// An at-least-once queue may deliver the same envelope twice. The ledger answers
		// once per commandId, before any handler runs.
		if !ledger.admit(cmd.CommandID, receivedAt) {
			if reportErr := report(ctx, redisClient, c.MachineID, cmd, auditDuplicate, "commandId was already handled", nil, receivedAt, time.Now().UTC()); reportErr != nil {
				return reportErr
			}
			logger.Printf("%s skipped: duplicate commandId %s", cmd.Action, cmd.CommandID)
			continue
		}
		result, err := spec.execute(c, cmd.arguments)
		completedAt := time.Now().UTC()
		if err != nil {
			if reportErr := report(ctx, redisClient, c.MachineID, cmd, auditFailed, err.Error(), nil, receivedAt, completedAt); reportErr != nil {
				return reportErr
			}
			logger.Printf("%s failed: %v", cmd.Action, err)
			continue
		}
		if err := report(ctx, redisClient, c.MachineID, cmd, auditSucceeded, "", result, receivedAt, completedAt); err != nil {
			return err
		}
		logger.Printf("%s completed: %+v", cmd.Action, result)
	}
	return ctx.Err()
}

func main() {
	configPath := flag.String("config", "config.json", "path to agent configuration")
	flag.Parse()
	c, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(c.LogFile), 0750); err != nil {
		log.Fatal(err)
	}
	file, err := os.OpenFile(c.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	logger := log.New(io.MultiWriter(os.Stdout, file), "anydesk-agent ", log.LstdFlags|log.LUTC)
	// The ledger outlives reconnections: a redelivery after a transport failure is the
	// most likely source of a duplicate commandId.
	ledger := newCommandLedger(dedupWindow, dedupMaxEntries)
	for wait := time.Second; ; wait = min(wait*2, reconnectMaxWait) {
		err := consume(context.Background(), c, ledger, logger)
		logger.Printf("transport failure; reconnecting in %s: %v", wait, err)
		time.Sleep(wait)
	}
}
