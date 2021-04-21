# ZMQ Address Monitoring

This tool monitors for txs with the specified addresses and then posts msgs to Slack (via a webhook) with links to an
explorer if they're encountered. The application automatically reconnects to the ZMQ socket should the target node go
offline.

Run in docker:

```
docker run -it --rm addrmonitor /opt/app \ 
-addrs="ADDRESSA...,ADDRESSB..." \
-slackWebhookURI="https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX" \
-node="tcp://<your-nodes-zmq-uri>:5556"
```

Usage:

```
  -addrs string
        the addresses to monitor for (comma separated, 81 tryte addrs)
  -connRetryInterval string
        the interval at which to dial back to the remote host in case of connection closure (default "5s")
  -dialTimeout string
        the dial timeout to the specified URI (default "5s")
  -explorerAddrsURI string
        defines the explorer URI for links for addresses (default "https://explorer.iota.org/mainnet/address")
  -explorerBundleURI string
        defines the explorer URI for links for bundles (default "https://explorer.iota.org/mainnet/bundle")
  -explorerTxsURI string
        defines the explorer URI for links for txs (default "https://explorer.iota.org/mainnet/transaction")
  -logAnySeenTx
        whether to output every seen txs to stdout
  -node string
        the URI to the ZMQ stream (default "tcp://example.com:5556")
  -onlyValue
        whether to only validate value transactions
  -slackWebhookURI string
        the webhook URI to which monitoring msgs are sent to
```