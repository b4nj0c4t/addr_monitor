package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-zeromq/zmq4"
	"github.com/iotaledger/iota.go/transaction"
)

var (
	nodeURI              = flag.String("node", "tcp://example.com:5556", "the URI to the ZMQ stream")
	logAnySeenTxs        = flag.Bool("logAnySeenTx", false, "whether to output every seen txs to stdout")
	connRetryIntervalStr = flag.String("connRetryInterval", "5s", "the interval at which to dial back to the remote host in case of connection closure")
	dialTimeoutStr       = flag.String("dialTimeout", "5s", "the dial timeout to the specified URI")
	monitorAddrsStr      = flag.String("addrs", "", "the addresses to monitor for (comma separated, 81 tryte addrs)")
	slackWebhookURI      = flag.String("slackWebhookURI", "", "the webhook URI to which monitoring msgs are sent to")
	monitorOnlyValueTx   = flag.Bool("onlyValue", false, "whether to only validate value transactions")
	txExplorerURI        = flag.String("explorerTxsURI", "https://explorer.iota.org/mainnet/transaction", "defines the explorer URI for links for txs")
	bundleExplorerURI    = flag.String("explorerBundleURI", "https://explorer.iota.org/mainnet/bundle", "defines the explorer URI for links for bundles")
	addrExplorerURI      = flag.String("explorerAddrsURI", "https://explorer.iota.org/mainnet/address", "defines the explorer URI for links for addresses")
)

const (
	trytesSubTopic = "trytes"
)

func mustParseDuration(str string, name string) time.Duration {
	dur, err := time.ParseDuration(str)
	if err != nil {
		log.Fatalf("unable to parse %s string '%s': %s", name, str, err)
	}
	return dur
}

func main() {
	flag.Parse()

	connRetryInterval := mustParseDuration(*connRetryIntervalStr, "connection retry interval")
	dialTimeout := mustParseDuration(*dialTimeoutStr, "dial timeout")

	monitorAddrSplit := strings.Split(*monitorAddrsStr, ",")
	monitorAddrs := make(map[string]struct{})
	for _, monitorAddr := range monitorAddrSplit {
		monitorAddrs[strings.TrimSpace(monitorAddr)] = struct{}{}
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancelFunc := context.WithCancel(context.Background())
	go func() {
		<-sigs
		cancelFunc()
	}()

	sub := zmq4.NewSub(ctx, zmq4.WithDialerTimeout(dialTimeout), zmq4.WithDialerRetry(1))
	defer func() {
		if err := sub.Close(); err != nil {
			log.Printf("could not close ZMQ socket successfully: %s", err)
		}
	}()

	log.Printf("dialing to ZMQ socket %s", *nodeURI)
	if err := sub.Dial(*nodeURI); err != nil {
		log.Fatalf("can't dial ZMQ URI: %s", err)
	}

	log.Printf("subscribing to '%s' topic", trytesSubTopic)
	if err := sub.SetOption(zmq4.OptionSubscribe, trytesSubTopic); err != nil {
		log.Fatalf("subscription failed: %s", err)
	}

	log.Println("address watcher started")
	defer log.Println("address watcher shutdown")
	for ctx.Err() == nil {
		msg, err := sub.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("could not receive message: %v", err)
				continue
			}

			log.Println("the remote server closed the connection")
			reconnect(sub, connRetryInterval)
			log.Println("successfully reconnected")
			continue
		}

		tx, err := extractTransaction(string(msg.Bytes()))
		if err != nil {
			log.Printf("unable to parse transaction from ZMQ stream: %s", err)
			continue
		}

		if tx.Value == 0 && *monitorOnlyValueTx {
			continue
		}

		if _, monitored := monitorAddrs[tx.Address]; monitored {
			log.Printf("seen tx %s on monitored address %s", tx.Hash, tx.Address)
			if err := sendSlackMessage(tx.Hash, tx.Address, tx.Bundle); err != nil {
				log.Printf("could not send slack webhook payload: %s", err)
			}
			continue
		}

		if *logAnySeenTxs {
			log.Println(tx.Hash, tx.Address)
		}
	}
}

type slackWebhookPayload struct {
	Text string `json:"text"`
}

var webhooktemplate = `monitoring:
- saw tx <%s|%s>
- address <%s|%s>
- bundle <%s|%s>
`

func sendSlackMessage(txHash string, address string, bundle string) error {
	txURI := fmt.Sprintf("%s/%s", *txExplorerURI, txHash)
	bundleURI := fmt.Sprintf("%s/%s", *bundleExplorerURI, bundle)
	addrURI := fmt.Sprintf("%s/%s", *addrExplorerURI, address)
	jsonWebHookPayload, err := json.Marshal(&slackWebhookPayload{
		Text: fmt.Sprintf(webhooktemplate, txURI, txHash, addrURI, address, bundleURI, bundle)},
	)
	if err != nil {
		return fmt.Errorf("unable to serialize slack webhook payload: %w", err)
	}
	res, err := http.Post(*slackWebhookURI, "application/json", bytes.NewReader(jsonWebHookPayload))
	if err != nil {
		return fmt.Errorf("unable to POST slack webhook payload: %w", err)
	}
	if res.StatusCode != 200 {
		defer res.Body.Close()
		bodyContent, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("unable to extract error from response content from POSTing slack webhook payload: %w", err)
		}
		return fmt.Errorf("unable to POST slack webhook payload: %s", bodyContent)
	}

	return nil
}

func reconnect(sub zmq4.Socket, connRetryInterval time.Duration) {
	for {
		log.Println("trying to reconnect...")
		if err := sub.Dial(*nodeURI); err != nil {
			log.Printf("dial attempt failed: %s...retrying in %v", err, connRetryInterval)
			time.Sleep(connRetryInterval)
			continue
		}
		if err := sub.SetOption(zmq4.OptionSubscribe, "trytes"); err != nil {
			log.Printf("subscription failed: %s...retrying in %v", err, connRetryInterval)
			time.Sleep(connRetryInterval)
			continue
		}
		break
	}
}

func extractTransaction(trytesTopicFrame string) (*transaction.Transaction, error) {
	trytesTopicFrame = strings.TrimPrefix(trytesTopicFrame, "trytes ")
	frameSplit := strings.Split(trytesTopicFrame, " ")
	tx, err := transaction.AsTransactionObject(frameSplit[0], frameSplit[1])
	if err != nil {
		return nil, err
	}
	return tx, nil
}
