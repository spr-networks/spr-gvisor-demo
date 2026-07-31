#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

echo "[1/6] Validating plugin manifest"
jq -e '
  .Name == "spr-tamago-demo" and
  .Runtime == "kvm" and
  .UnixPath == "/state/plugins/spr-tamago-demo/socket.sock" and
  .HasUI == true and
  .SandboxedUI == true and
  .Enabled == true and
  .NetworkCapabilities.Interface == "spr-tamago" and
  .NetworkCapabilities.DeviceMAC == "02:53:50:52:54:47" and
  .NetworkCapabilities.Policies == []
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
  .services["spr-tamago-demo-kernel"].runtime == "spr-krun" and
  .services["spr-tamago-demo-kernel"].annotations["krun.cpus"] == "1" and
  .services["spr-tamago-demo-kernel"].annotations["krun.ram_mib"] == "256" and
  .services["spr-tamago-demo-kernel"].annotations["krun.kernel_path"] == "/tamago-kernel" and
  .services["spr-tamago-demo-kernel"].annotations["krun.kernel_format"] == "0" and
  .services["spr-tamago-demo-kernel"].annotations["krun.tap_name"] == "kruntap0" and
  .services["spr-tamago-demo-kernel"].annotations["krun.net_uplink"] == "eth0" and
  (.services["spr-tamago-demo-kernel"].devices | length) == 1 and
  .services["spr-tamago-demo-gateway"].environment.TAMAGO_URL == "http://192.0.2.2:8080" and
  .networks.tamagonet.internal == true and
  .networks.tamagonet.ipam.config[0].subnet == "192.0.2.0/24"
' <<<"${compose_json}" >/dev/null

echo "[5/6] Checking direct-kernel inputs"
jq -e '
  .kernel_path == "/tamago-kernel" and
  .kernel_format == 0 and
  .ram_mib == 256
' .krun_vm.json >/dev/null
rg -q 'Direct-booted kernel . no Linux guest' kernel/main.go
rg -q 'github.com/usbarmory/go-net/virtio' kernel/main.go
rg -q 'GOOS=tamago GOOSPKG=github.com/usbarmory/tamago GOARCH=arm64' Dockerfile
test -f overlays/virtio_arm64.go
test -f overlays/virtio_arm64_empty.go
test ! -e kernel/virtionet/net.go

echo "[6/6] Checking CI and reproducibility targets"
rg -q '^FROM scratch AS reproducibility$' Dockerfile
rg -q '^  reproducibility:$' .github/workflows/ci.yml
rg -q 'rewrite-timestamp=true' build_docker_compose.sh reproducibility_test.sh

echo "All checks passed."
