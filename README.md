# StreamingFast Firehose for EOSIO
[![reference](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat-square)](https://pkg.go.dev/github.com/streamingfast/playground-firehose-eosio-go)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

Benefits:
* single integration for accessing history and real-time, low-latency stream of live block data
* simplify your code that handle the irregularities of blockchains (our customers said 90% less code is needed when using this system)
* get rid of your complex message queues deployments
  * have your Kafka consumers talk directly to the Firehose, less latency, and simpler
  * replace RabbitMQ, and have consumers filter their topics directly from the Firehose
  * extremely precise and fork-aware checkpointing for consumers


## Authenticate

See documentation: https://docs.dfuse.io/eosio/public-apis/reference/authentication/

In bash:

```bash
DFUSE_KEY=server_YOUR_API_KEY_FROM_THE_DFUSE_PORTAL

export DFUSE_TOKEN=$(curl https://auth.dfuse.io/v1/auth/issue -s --data-binary '{"api_key":"'$DFUSE_KEY'", "lifetime": 86400000}' | jq -r .token);
```

In Python: https://github.com/dfuse-io/docs/blob/master/samples/python/eos/graphql-grpc/example.py#L14-L33


## Download the service protobuf

```
curl -O https://raw.githubusercontent.com/dfuse-io/proto/develop/google/protobuf/any.proto
curl -O https://raw.githubusercontent.com/dfuse-io/proto/develop/google/protobuf/timestamp.proto
curl -O https://raw.githubusercontent.com/dfuse-io/proto/develop/dfuse/bstream/v1/bstream.proto
curl -O https://raw.githubusercontent.com/dfuse-io/proto-eosio/master/dfuse/eosio/codec/v1/codec.proto
```


## Try it from the CLI

Stream of irreversble blocks from the past:

```
    grpcurl -H "Authorization: Bearer $DFUSE_TOKEN" \
       -d '{"start_block_num": 138000000, "stop_block_num": 139000000, "fork_steps": ["STEP_IRREVERSIBLE"], "include_filter_expr": ""}' \
       -import-path . \
       -proto bstream.proto \
       -proto codec.proto \
       $SERVICE_ENDPOINT:443 \
       dfuse.bstream.v1.BlockStreamV2.Blocks | jq .

```

Use `include_filter_expr` to choose exactly what you need.

If the connection is interrupted, your process crashes or whatever, continue where you left off by passing the last `block_num` you received back into `start_block_num` (+1)

```
    grpcurl -H "Authorization: Bearer $DFUSE_TOKEN" \
       -d '{"start_block_num": 138500000, "stop_block_num": 139000000, "fork_steps": ["STEP_IRREVERSIBLE"], "include_filter_expr": ""}' \
       -import-path . \
       -proto bstream.proto \
       -proto codec.proto \
       $SERVICE_ENDPOINT:443 \
       dfuse.bstream.v1.BlockStreamV2.Blocks | jq .
```


## Query language

The language used in the `include_filter_expr` and `exclude_filter_expr` is
a Common Expression Language expression, as defined here:

https://github.com/google/cel-spec/blob/master/doc/langdef.md

Queries match on individual actions, and transactions are returned if
one action matches the filter query.  You can then check the
`filtering_matched` field on the `ActionTrace` protobuf object to make
sure they are relevant to your query.

Fields that are available for filtering:

* **`auth`**: a list of strings, authorizers for a given action
* **`receiver`**: string, the contract being executed
* **`action`**: string, the name of the action being executed
* **`input`**: bool, whether this is a top-level action
* **`notif`**: bool, whether this is a notification
* **`data['field']`**: any, matches some data from the action parameters


### Sample queries

Match transactions that notified or executed on a list of contracts:

```
account in ['newdexpublic','dice.bg','wallet.bg','pokerwar.bg','bulls.bg','diceproxy.bg','texas.bg','slot.bg','bonus.bg','dividend.bg','candy.bg','miner.bg','giver.bg','threecard.bg','swap.defi']
```

Match transactions that werw authorized (signed) by certain accounts:

```
auth.exists (x, x in ['newdexpublic','dice.bg','wallet.bg','pokerwar.bg','bulls.bg','diceproxy.bg','texas.bg','slot.bg','bonus.bg','dividend.bg','candy.bg','miner.bg','giver.bg','threecard.bg','swap.defi'])
```


Ignore some of the mining activity on chain:

```
!(trx_action_count > 200 && top5_trx_actors.exists(x, x in ['eosiopowcoin','eidosonecoin','mine4charity']))
```

Here, in combination:

```
!(trx_action_count > 200 && top5_trx_actors.exists(x, x in ['eosiopowcoin','eidosonecoin','mine4charity'])) && (account in ['newdexpublic','dice.bg','wallet.bg','pokerwar.bg','bulls.bg','diceproxy.bg','texas.bg','slot.bg','bonus.bg','dividend.bg','candy.bg','miner.bg','giver.bg','threecard.bg','swap.defi'] || receiver in ['newdexpublic','dice.bg','wallet.bg','pokerwar.bg','bulls.bg','diceproxy.bg','texas.bg','slot.bg','bonus.bg','dividend.bg','candy.bg','miner.bg','giver.bg','threecard.bg','swap.defi'] || auth.exists (x, x in ['newdexpublic','dice.bg','wallet.bg','pokerwar.bg','bulls.bg','diceproxy.bg','texas.bg','slot.bg','bonus.bg','dividend.bg','candy.bg','miner.bg','giver.bg','threecard.bg','swap.defi']))
```
## Contributing

**Issues and PR in this repo related strictly to Firehose for EOSIO.**

Report any protocol-specific issues in their
[respective repositories](https://github.com/streamingfast/streamingfast#protocols)

**Please first refer to the general
[StreamingFast contribution guide](https://github.com/streamingfast/streamingfast/blob/master/CONTRIBUTING.md)**,
if you wish to contribute to this code base.

## License

[Apache 2.0](LICENSE)