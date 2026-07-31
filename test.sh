#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/spr-tamago-go-cache}"

echo "[1/6] Validating plugin manifest"
jq -e '
  .Name == "spr-tamago-demo" and
  .Runtime == "kvm" and
  .UnixPath == "/state/plugins/spr-tamago-demo/socket.sock" and
  .HasUI == true and
  .SandboxedUI == true and
  .Enabled == true and
  (has("NetworkCapabilities") | not)
' plugin.json >/dev/null

echo "[2/6] Validating reproducible inputs and shell scripts"
bash -n build_docker_compose.sh reproducibility_test.sh test.sh verify_reproducible.sh
./verify_reproducible.sh

echo "[3/6] Verifying and testing Go modules"
go mod verify
go test ./...

echo "[4/6] Validating Compose"
docker compose -f docker-compose.yml config --quiet
compose_json="$(docker compose -f docker-compose-kvm.yml config --format json)"
jq -e '
  (.services | length) == 1 and
  .services["spr-tamago-demo"].runtime == "spr-krun" and
  .services["spr-tamago-demo"].annotations["krun.cpus"] == "1" and
  .services["spr-tamago-demo"].annotations["krun.ram_mib"] == "256" and
  .services["spr-tamago-demo"].annotations["krun.kernel_path"] == "/tamago-kernel" and
  .services["spr-tamago-demo"].annotations["krun.kernel_format"] == "0" and
  .services["spr-tamago-demo"].annotations["krun.vsock_path"] == "/state/plugins/spr-tamago-demo/socket.sock" and
  .services["spr-tamago-demo"].annotations["krun.vsock_port"] == "4040" and
  (.services["spr-tamago-demo"].annotations | has("krun.tap_name") | not) and
  (.services["spr-tamago-demo"].annotations | has("krun.net_uplink") | not) and
  (.services["spr-tamago-demo"] | has("devices") | not)
' <<<"${compose_json}" >/dev/null

echo "[5/6] Checking direct-kernel inputs"
jq -e '
  .kernel_path == "/tamago-kernel" and
  .kernel_format == 0 and
  .ram_mib == 256
' .krun_vm.json >/dev/null
grep -Fq 'Direct-booted kernel · no Linux guest' kernel/main.go
grep -Fq 'virtio-vsock' kernel/main.go
grep -Fq 'DeviceID = 19' kernel/vsock/protocol.go
grep -Fq 'func printk(_ byte) {}' kernel/runtime.go
! grep -Fq 'pl011Base' kernel/runtime.go
grep -Fq 'krun.vsock_path: "/state/plugins/spr-tamago-demo/socket.sock"' docker-compose-kvm.yml
grep -Fq 'GOOS=tamago GOOSPKG=github.com/usbarmory/tamago GOARCH=arm64' Dockerfile
test -f overlays/virtio_arm64.go
test -f overlays/virtio_arm64_empty.go
test ! -e kernel/virtionet/net.go
test ! -e gateway.go
test ! -e gateway_test.go
! grep -REq '192\.0\.2\.|ipv4_address:|TAMAGO_URL|spr-tamago-demo-gateway' \
  docker-compose.yml docker-compose-kvm.yml plugin.json Dockerfile

echo "[6/6] Checking CI and reproducibility targets"
grep -Eq '^FROM scratch AS reproducibility$' Dockerfile
grep -Eq '^  reproducibility:$' .github/workflows/ci.yml
grep -Fq 'rewrite-timestamp=true' build_docker_compose.sh reproducibility_test.sh

echo "All checks passed."
