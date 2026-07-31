//go:build tamago && arm64

#include "textflag.h"

// Go reserves x18 for the platform and x28 for the current goroutine. Both
// still form part of the architectural EL0 register set, so name them using
// the spellings accepted by the Go assembler.
#define R18 R18_PLATFORM
#define R28 g

#define U_R0       0
#define U_R2       16
#define U_R4       32
#define U_R6       48
#define U_R8       64
#define U_R10      80
#define U_R12      96
#define U_R14      112
#define U_R16      128
#define U_R18      144
#define U_R20      160
#define U_R22      176
#define U_R24      192
#define U_R26      208
#define U_R28      224
#define U_R30      240
#define U_SP       248
#define U_PC       256
#define U_PSTATE   264
#define K_TTBR     272
#define A_TTBR     280
#define K_SP       288
#define EX_SP      296
#define K_R19      304
#define K_R21      320
#define K_R23      336
#define K_R25      352
#define K_R27      368
#define K_R29      384
#define S_ESR      400
#define S_FAR      408
#define S_VECTOR   416
#define U_TLS      424

// runUser enters EL0 and returns after any synchronous lower-EL exception.
TEXT ·runUser(SB),NOSPLIT,$0-8
	MOVD state+0(FP), R18
	ORR $0xffff000000000000, R18, R18
	MOVD RSP, R16
	MOVD R16, K_SP(R18)
	STP (R19, R20), K_R19(R18)
	STP (R21, R22), K_R21(R18)
	STP (R23, R24), K_R23(R18)
	STP (R25, R26), K_R25(R18)
	STP (R27, R28), K_R27(R18)
	STP (R29, R30), K_R29(R18)
	MSR R18, TPIDR_EL1

	MOVD U_SP(R18), R16
	MSR R16, SP_EL0
	MOVD U_PC(R18), R16
	MSR R16, ELR_EL1
	MOVD U_PSTATE(R18), R16
	MSR R16, SPSR_EL1
	MOVD U_TLS(R18), R16
	MSR R16, TPIDR_EL0
	MOVD EX_SP(R18), R16
	MOVD R16, RSP
	MOVD A_TTBR(R18), R16
	MSR R16, TTBR0_EL1
	ISB $15

	LDP U_R0(R18), (R0, R1)
	LDP U_R2(R18), (R2, R3)
	LDP U_R4(R18), (R4, R5)
	LDP U_R6(R18), (R6, R7)
	LDP U_R8(R18), (R8, R9)
	LDP U_R10(R18), (R10, R11)
	LDP U_R12(R18), (R12, R13)
	LDP U_R14(R18), (R14, R15)
	LDP U_R16(R18), (R16, R17)
	LDP U_R20(R18), (R20, R21)
	LDP U_R22(R18), (R22, R23)
	LDP U_R24(R18), (R24, R25)
	LDP U_R26(R18), (R26, R27)
	LDP U_R28(R18), (R28, R29)
	MOVD U_R30(R18), R30
	LDP U_R18(R18), (R18, R19)
	WORD $0xd69f03e0 // eret

// lowerSync is VBAR slot 8: synchronous exception from AArch64 lower EL.
TEXT ·lowerSync(SB),NOSPLIT|NOFRAME,$0
	SUB $16, RSP, RSP
	STP (R18, R19), 0(RSP)
	MRS TPIDR_EL1, R18
	STP (R0, R1), U_R0(R18)
	STP (R2, R3), U_R2(R18)
	STP (R4, R5), U_R4(R18)
	STP (R6, R7), U_R6(R18)
	STP (R8, R9), U_R8(R18)
	STP (R10, R11), U_R10(R18)
	STP (R12, R13), U_R12(R18)
	STP (R14, R15), U_R14(R18)
	STP (R16, R17), U_R16(R18)
	LDP 0(RSP), (R16, R17)
	STP (R16, R17), U_R18(R18)
	ADD $16, RSP, RSP
	STP (R20, R21), U_R20(R18)
	STP (R22, R23), U_R22(R18)
	STP (R24, R25), U_R24(R18)
	STP (R26, R27), U_R26(R18)
	STP (R28, R29), U_R28(R18)
	MOVD R30, U_R30(R18)
	MRS SP_EL0, R16
	MOVD R16, U_SP(R18)
	MRS ELR_EL1, R16
	MOVD R16, U_PC(R18)
	MRS SPSR_EL1, R16
	MOVD R16, U_PSTATE(R18)
	MRS TPIDR_EL0, R16
	MOVD R16, U_TLS(R18)
	MRS ESR_EL1, R16
	MOVD R16, S_ESR(R18)
	MRS FAR_EL1, R16
	MOVD R16, S_FAR(R18)
	MOVD $8, R16
	MOVD R16, S_VECTOR(R18)

	MOVD K_TTBR(R18), R16
	MSR R16, TTBR0_EL1
	ISB $15
	MOVD K_SP(R18), R16
	MOVD R16, RSP
	LDP K_R19(R18), (R19, R20)
	LDP K_R21(R18), (R21, R22)
	LDP K_R23(R18), (R23, R24)
	LDP K_R25(R18), (R25, R26)
	LDP K_R27(R18), (R27, R28)
	LDP K_R29(R18), (R29, R30)
	RET

// All other vector slots park in a WFE loop; the demo masks external
// interrupts and only enters through lowerSync.
TEXT ·unexpectedVector(SB),NOSPLIT|NOFRAME,$0
	WFE
	WORD $0x17ffffff // b back to WFE

TEXT ·bareVectors(SB),NOSPLIT|NOFRAME,$0
	PCALIGN $2048
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·lowerSync(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)
	PCALIGN $128
	B ·unexpectedVector(SB)

TEXT ·addrOfVectors(SB),NOSPLIT,$0-8
	MOVD $·bareVectors(SB), R0
	MOVD R0, ret+0(FP)
	RET

TEXT ·addrOfSmokeEntry(SB),NOSPLIT,$0-8
	MOVD $·smokeEntry(SB), R0
	MOVD R0, ret+0(FP)
	RET

// Linux/AArch64 write(1, 0x500000, 32), followed by exit(0).
TEXT ·smokeEntry(SB),NOSPLIT|NOFRAME,$0
	WORD $0xd2800020 // mov x0, #1
	WORD $0xd2800001 // mov x1, #0
	WORD $0xf2a00a01 // movk x1, #0x50, lsl #16
	WORD $0xd2800402 // mov x2, #32
	WORD $0xd2800808 // mov x8, #64
	WORD $0xd4000001 // svc #0
	WORD $0xd2800000 // mov x0, #0
	WORD $0xd2800ba8 // mov x8, #93
	WORD $0xd4000001 // svc #0

// installMMU atomically changes from TamaGo's 39-bit TTBR0-only layout to
// gVisor ring0's 48-bit split layout. Identity mapping makes the short MMU-off
// window safe.
TEXT ·installMMU(SB),NOSPLIT,$0-48
	MOVD ttbr0+0(FP), R0
	MOVD ttbr1+8(FP), R1
	MOVD tcr+16(FP), R2
	MOVD mair+24(FP), R3
	MOVD vbar+32(FP), R4
	MOVD tpidr+40(FP), R5

	DSB $15
	ISB $15
	MRS SCTLR_EL1, R6
	BIC $1, R6
	MSR R6, SCTLR_EL1
	ISB $15

	MSR R3, MAIR_EL1
	MSR R2, TCR_EL1
	MSR R0, TTBR0_EL1
	MSR R1, TTBR1_EL1
	MSR R4, VBAR_EL1
	MSR R5, TPIDR_EL1
	TLBI VMALLE1
	DSB $15
	ISB $15

	ORR $1, R6
	MSR R6, SCTLR_EL1
	ISB $15
	RET
