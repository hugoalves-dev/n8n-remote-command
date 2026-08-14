package main

import (
	"testing"
	"time"
)

func TestLedgerAdmitsACommandItHasNotSeen(t *testing.T) {
	ledger := newCommandLedger(time.Hour, 10)

	if !ledger.admit("8ddc0c10-253a-4e37-895f-c4b749e1c312", time.Now().UTC()) {
		t.Fatal("expected an unseen commandId to be admitted")
	}
}

func TestLedgerRefusesARepeatedCommandID(t *testing.T) {
	now := time.Now().UTC()
	ledger := newCommandLedger(time.Hour, 10)
	ledger.admit("8ddc0c10-253a-4e37-895f-c4b749e1c312", now)

	if ledger.admit("8ddc0c10-253a-4e37-895f-c4b749e1c312", now.Add(time.Second)) {
		t.Fatal("expected a repeated commandId to be refused")
	}
}

func TestLedgerForgetsEntriesOlderThanItsWindow(t *testing.T) {
	now := time.Now().UTC()
	ledger := newCommandLedger(time.Hour, 10)
	ledger.admit("8ddc0c10-253a-4e37-895f-c4b749e1c312", now)

	if !ledger.admit("8ddc0c10-253a-4e37-895f-c4b749e1c312", now.Add(time.Hour+time.Second)) {
		t.Fatal("expected an entry older than the window to be forgotten")
	}
}

func TestLedgerStaysWithinItsSizeBound(t *testing.T) {
	now := time.Now().UTC()
	ledger := newCommandLedger(time.Hour, 2)

	ledger.admit("first", now)
	ledger.admit("second", now.Add(time.Second))
	ledger.admit("third", now.Add(2*time.Second))

	if size := ledger.size(); size != 2 {
		t.Fatalf("ledger.size() = %d, want 2", size)
	}
	if ledger.admit("third", now.Add(3*time.Second)) {
		t.Fatal("expected the newest entry to be kept")
	}
}
