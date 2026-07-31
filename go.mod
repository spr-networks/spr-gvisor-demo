module github.com/spr-networks/spr-gvisor-demo

go 1.26.4

tool github.com/usbarmory/tamago/cmd/tamago

require (
	github.com/usbarmory/go-net v0.0.0-20260714134120-c2c964e7084c
	github.com/usbarmory/tamago v1.26.5-0.20260626120227-bb8159e64f82
)

require github.com/soypat/lneto v0.2.0 // indirect

require (
	github.com/google/btree v1.1.2 // indirect
	golang.org/x/exp v0.0.0-20250711185948-6ae5c78190dc // indirect
	golang.org/x/sys v0.43.0
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gvisor.dev/gvisor v0.0.0-20260730080753-99012c9af411
)
