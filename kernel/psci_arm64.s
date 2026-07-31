//go:build tamago && arm64

#include "textflag.h"

// PSCI_0_2_FN_SYSTEM_OFF using libkrun's HVC conduit.
TEXT ·psciSystemOff(SB), NOSPLIT, $0-0
	MOVD $0x84000008, R0
	HVC  $0
	RET
