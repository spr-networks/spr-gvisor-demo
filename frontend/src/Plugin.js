import React, { useCallback, useEffect, useState } from 'react'
import {
  api,
  Button,
  ButtonText,
  Card,
  HStack,
  KeyVal,
  ListHeader,
  Loading,
  Page,
  SectionHeader,
  StatTile,
  StatusDot,
  Text,
  VStack
} from '@spr-networks/plugin-ui'

const PLUGIN_BASE = `/plugins/${api.pluginURI() || 'spr-gvisor-demo'}`
const REFRESH_INTERVAL_MS = 5000

const textOrDash = (value) => value || '—'

export default function Plugin() {
  const [status, setStatus] = useState(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(() => {
    return api
      .get(`${PLUGIN_BASE}/status`)
      .then((next) => {
        setStatus(next)
        setError('')
      })
      .catch((err) => {
        setError(`Unable to read kernel status${err?.status ? ` (${err.status})` : ''}.`)
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    refresh()
    const timer = window.setInterval(refresh, REFRESH_INTERVAL_MS)
    return () => window.clearInterval(timer)
  }, [refresh])

  if (loading && !status) {
    return (
      <Page>
        <Loading text="Connecting to gVisor Sentry..." />
      </Page>
    )
  }

  const network = status?.network || {}
  const sentryReady = status?.gvisor === 'ready'
  const networkOnline = network.phase === 'online'

  return (
    <Page>
      <ListHeader
        title="gVisor Kernel Demo"
        description="gVisor Sentry as the application kernel under krun — no Linux kernel"
        mark="gv"
        status={sentryReady ? 'Sentry ready' : textOrDash(status?.gvisor)}
        statusAction={sentryReady ? 'success' : 'warning'}
      >
        <Button size="sm" variant="outline" onPress={refresh}>
          <ButtonText>Refresh</ButtonText>
        </Button>
      </ListHeader>

      <Card>
        <SectionHeader
          title="Hello, SPR."
          right={<StatusDot online={sentryReady} warn={!!status && !sentryReady} />}
        />
        <Text
          size="lg"
          fontWeight="$semibold"
          color="$primary700"
          sx={{ _dark: { color: '$primary300' }, '@base': { fontFamily: 'monospace' } }}
        >
          {textOrDash(status?.output?.trim())}
        </Text>
        <Text mt="$2" size="sm" color="$muted500">
          Captured from the embedded Linux/AArch64 task running inside gVisor
          Sentry. This UI uses the SPR Plugin UI SDK and is embedded in the direct-boot image.
        </Text>
      </Card>

      <Card tone={status?.error ? 'warning' : 'default'}>
        <SectionHeader title="Application kernel" />
        <HStack flexWrap="wrap" gap="$2">
          <StatTile label="gVisor" value={textOrDash(status?.gvisor_version)} mono />
          <StatTile label="Sentry" value={textOrDash(status?.gvisor)} />
          <StatTile label="TamaGo" value={textOrDash(status?.tamago_version)} mono />
          <StatTile label="Runtime" value={`${textOrDash(status?.runtime)}/${textOrDash(status?.arch)}`} mono />
        </HStack>
        <VStack mt="$4" space="sm">
          <KeyVal label="Role" value="krun guest · Linux ABI at EL0" />
          <KeyVal label="SPR IPC" value={`${textOrDash(status?.ipc)} · port ${status?.port || '—'}`} mono />
          {status?.error ? <KeyVal label="Sentry detail" value={status.error} /> : null}
        </VStack>
      </Card>

      <Card tone={error ? 'warning' : 'default'}>
        <SectionHeader
          title="Sentry VirtIO network"
          right={<StatusDot online={networkOnline} warn={!!status && !networkOnline} />}
        />
        <VStack space="sm">
          <KeyVal label="Owner" value={textOrDash(network.owner)} />
          <KeyVal label="State" value={textOrDash(network.phase)} />
          <KeyVal label="Interface" value={`${textOrDash(network.device)} · ${textOrDash(network.mac)}`} mono />
          <KeyVal label="DHCP address" value={textOrDash(network.address)} mono />
          <KeyVal label="Gateway" value={textOrDash(network.gateway)} mono />
          <KeyVal label="DNS" value={network.dns?.length ? network.dns.join(', ') : '—'} mono />
          <KeyVal label="Lease" value={textOrDash(network.lease)} />
          <KeyVal label="Internet probe" value={textOrDash(network.probe)} />
          {network.error ? <KeyVal label="Network detail" value={network.error} /> : null}
          {error ? <Text color="$amber700" sx={{ _dark: { color: '$amber300' } }}>{error}</Text> : null}
        </VStack>
      </Card>
    </Page>
  )
}
