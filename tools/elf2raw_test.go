package main

import "testing"

func TestBranchInstruction(t *testing.T) {
	got, err := branchInstruction(0x80000000, 0x80010000)
	if err != nil {
		t.Fatal(err)
	}
	if want := uint32(0x14004000); got != want {
		t.Fatalf("instruction = %#08x, want %#08x", got, want)
	}
}

func TestBranchInstructionRejectsUnalignedTarget(t *testing.T) {
	if _, err := branchInstruction(0x80000000, 0x80010002); err == nil {
		t.Fatal("unaligned branch unexpectedly succeeded")
	}
}
