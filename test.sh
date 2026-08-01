#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/spr-gvisor-go-cache}"

echo "[1/6] Validating plugin manifest"
jq -e '
  .Name == "spr-gvisor-demo" and
  .Runtime == "kvm" and
  .UnixPath == "/state/plugins/spr-gvisor-demo/socket.sock" and
  .HasUI == true and
  .SandboxedUI == true and
  .Enabled == true and
  .NetworkCapabilities.Interface == "spr-gvisor-demo" and
  .NetworkCapabilities.DeviceMAC == "02:53:50:52:47:01" and
  .NetworkCapabilities.Policies == ["wan", "dns"]
' plugin.json >/dev/null

echo "[2/6] Validating reproducible inputs and shell scripts"
bash -n build_docker_compose.sh reproducibility_test.sh test.sh verify_reproducible.sh frontend/bundle.sh
./verify_reproducible.sh

echo "[3/6] Verifying and testing Go modules"
go mod verify
go test ./kernel/... ./overlays/... ./tools/...

echo "[4/6] Validating Compose"
docker compose -f docker-compose.yml config --quiet
compose_json="$(docker compose -f docker-compose-kvm.yml config --format json)"
jq -e '
  (.services | length) == 1 and
  .services["spr-gvisor-demo"].runtime == "spr-krun" and
  .services["spr-gvisor-demo"].pull_policy == "missing" and
  .services["spr-gvisor-demo"].annotations["krun.cpus"] == "1" and
  .services["spr-gvisor-demo"].annotations["krun.ram_mib"] == "256" and
  .services["spr-gvisor-demo"].annotations["krun.kernel_path"] == "/gvisor-kernel" and
  .services["spr-gvisor-demo"].annotations["krun.kernel_format"] == "0" and
  .services["spr-gvisor-demo"].annotations["krun.vsock_path"] == "/state/plugins/spr-gvisor-demo/socket.sock" and
  .services["spr-gvisor-demo"].annotations["krun.vsock_port"] == "4040" and
  .services["spr-gvisor-demo"].annotations["krun.tap_name"] == "kruntap0" and
  .services["spr-gvisor-demo"].annotations["krun.net_uplink"] == "eth0" and
  (.services["spr-gvisor-demo"].devices | any(.source == "/dev/net/tun" and .target == "/dev/net/tun")) and
  .networks.gvisornet.name == "spr-gvisor-demo" and
  .networks.gvisornet.driver_opts["com.docker.network.bridge.name"] == "spr-gvisor-demo" and
  .networks.gvisornet.driver_opts["com.docker.network.bridge.inhibit_ipv4"] == "true"
' <<<"${compose_json}" >/dev/null

echo "[5/6] Checking direct-kernel inputs"
jq -e '
  .kernel_path == "/gvisor-kernel" and
  .kernel_format == 0 and
  .ram_mib == 256
' .krun_vm.json >/dev/null
grep -Fq 'gVisor Sentry as the application kernel under krun' frontend/src/Plugin.js
grep -Fq "from '@spr-networks/plugin-ui'" frontend/src/index.js frontend/src/Plugin.js
grep -Fq '<PluginApp>' frontend/src/index.js
grep -Fq 'move-inline-scripts.js' frontend/bundle.sh Dockerfile
grep -Fq '//go:embed ui/index.html' kernel/main.go
grep -Fq 'tamago_version' kernel/main.go frontend/src/Plugin.js
grep -Fq 'gvisor_version' kernel/main.go frontend/src/Plugin.js
grep -Fq 'COPY --from=frontend /src/frontend/build/index.html ./kernel/ui/index.html' Dockerfile
! grep -REq 'Linux (in VM|kernel).*(none)' frontend/src
grep -Fq 'Hello World from gVisor Sentry!' kernel/gvisor_sentry.go
grep -Fq 'X-GVisor-Kernel: true' kernel/main.go
grep -Fq 'virtio-vsock' kernel/main.go
grep -Fq 'RootNetworkNamespace: rootNetworkNS' kernel/gvisor_sentry.go
grep -Fq 'DeviceID = 19' kernel/vsock/protocol.go
grep -Fq 'func printk(_ byte) {}' kernel/runtime.go
! grep -Fq 'pl011Base' kernel/runtime.go
grep -Fq 'krun.vsock_path: "/state/plugins/spr-gvisor-demo/socket.sock"' docker-compose-kvm.yml
grep -Fq 'GOOS=tamago GOOSPKG=github.com/usbarmory/tamago GOARCH=arm64' Dockerfile
grep -Fq -- '-replace=gvisor.dev/gvisor=/tmp/gvisor-tamago' Dockerfile
grep -Fq -- '-replace=golang.org/x/sys=/tmp/xsys-tamago' Dockerfile
test -f overlays/virtio_arm64.go
test -f overlays/virtio_arm64_empty.go
test -f overlays/gvisor/pkg/sentry/pgalloc/pgalloc_tamago.go
test -f overlays/xsys/unix/syscall_tamago.go
test -f tools/prepare_gvisor.go
test -f kernel/gvisorplatform/platform.go
test -f kernel/gvisorplatform/registers_arm64.s
grep -Fq 'sprDMAStart uint64 = 0x8c000000' tools/prepare_tamago.go
grep -Fq 'case addr >= sprDMAStart && addr < sprDMAEnd:' tools/prepare_tamago.go
test -f kernel/sprnet/dhcp.go
test -f kernel/sprnet/virtio_tamago.go
grep -Fq 'github.com/usbarmory/go-net/virtio' kernel/sprnet/virtio_tamago.go
grep -Fq 'DHCPDISCOVER and DHCPREQUEST' kernel/sprnet/dhcp.go
grep -Fq 'newSentryNetworkNamespace' kernel/network_tamago.go
grep -Fq 'Sentry netstack DNS + TCP example.com:80 succeeded' kernel/network_tamago.go
grep -Fq 'krun.tap_name: "kruntap0"' docker-compose-kvm.yml
grep -Fq 'krun.net_uplink: "eth0"' docker-compose-kvm.yml
test ! -e gateway.go
test ! -e gateway_test.go
! grep -REq 'tamago-kernel|SPR_TAMAGO_IMAGE' \
  Dockerfile docker-compose.yml docker-compose-kvm.yml .krun_vm.json build_docker_compose.sh .github/workflows/ci.yml
! grep -REq '192\.0\.2\.|ipv4_address:|TAMAGO_URL|spr-gvisor-demo-gateway' \
  docker-compose.yml docker-compose-kvm.yml plugin.json Dockerfile

echo "[6/6] Checking CI and reproducibility targets"
grep -Eq '^FROM scratch AS reproducibility$' Dockerfile
grep -Eq '^FROM \$\{NODE_REF\} AS frontend$' Dockerfile
grep -Eq '^  reproducibility:$' .github/workflows/ci.yml
grep -Fq 'rewrite-timestamp=true' build_docker_compose.sh reproducibility_test.sh

echo "All checks passed."
