package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dfuse-io/bstream"
	pbcodec "github.com/dfuse-io/dfuse-eosio/pb/dfuse/eosio/codec/v1"
	"github.com/dfuse-io/dgrpc"
	"github.com/dfuse-io/logging"
	pbbstream "github.com/dfuse-io/pbgo/dfuse/bstream/v1"
	"github.com/golang/protobuf/ptypes"
	"github.com/paulbellamy/ratecounter"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/oauth"
)

var retryDelay = 5 * time.Second
var statusFrequency = 15 * time.Second
var traceEnabled = logging.IsTraceEnabled("consumer", "github.com/dfuse-io/playground-firehose-go")
var zlog = logging.NewSimpleLogger("consumer", "github.com/dfuse-io/playground-firehose-go")

func init() {
	logging.TestingOverride()
}

func main() {
	token := os.Getenv("DFUSE_API_TOKEN")

	ensure(token != "", errorUsage("the environment variable DFUSE_API_TOKEN must be set to a valid dfuse API JWT token"))
	ensure(len(os.Args) >= 4, errorUsage("missing arguments"))

	endpoint := os.Args[1]
	filter := os.Args[2]
	blockRange := newBlockRange(os.Args[3])

	cursor := bstream.BlockRefEmpty
	conn, err := dgrpc.NewExternalClient(endpoint)
	noError(err, "unable to create external gRPC client to %q", endpoint)

	client := pbbstream.NewBlockStreamV2Client(conn)
	stats := newStats()
	nextStatus := time.Now().Add(statusFrequency)

	zlog.Info("Starting firehose test", zap.String("endpoint", endpoint), zap.String("filter", filter), zap.Stringer("range", blockRange))
stream:
	for {
		start := blockRange.start
		if cursor.Num() > start {
			start = cursor.Num()
		}

		credentials := oauth.NewOauthAccess(&oauth2.Token{AccessToken: token, TokenType: "Bearer"})
		stream, err := client.Blocks(context.Background(), &pbbstream.BlocksRequestV2{
			Decoded: true,
			// FIXME: This works only when streaming irreversible only, the full block reference should
			//        be used but Firehose has a bug around StartBlockId (wrong type)
			StartBlockNum:     int64(start),
			StopBlockNum:      blockRange.end,
			ExcludeStartBlock: false,
			ExcludeStopBlock:  true,
			HandleForks:       true,
			HandleForksSteps:  []pbbstream.ForkStep{pbbstream.ForkStep_STEP_IRREVERSIBLE},
			IncludeFilterExpr: filter,
		}, grpc.PerRPCCredentials(credentials))
		noError(err, "unable to start blocks stream")

		for {
			zlog.Debug("Waiting for message to reach us")
			response, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					if cursor.Num() >= blockRange.end-1 {
						// Complete the outer loop which terminates streaming blocks
						break stream
					}

					// FIXME: The firehose service has a bug where it can stop prior finishing the range, so until
					//        it's fixed, we are going to break our the loop and restart the stream to continue further.
					zlog.Warn("Stream closed while end range not reached, retrying", zap.Stringer("cursor", cursor), zap.Duration("retry_delay", retryDelay))
					break
				}

				zlog.Error("Stream encountered a remote error, going to retry", zap.Stringer("cursor", cursor), zap.Duration("retry_delay", retryDelay), zap.Error(err))
				break
			}

			zlog.Debug("Decoding received message's block")
			block := &pbcodec.Block{}
			err = ptypes.UnmarshalAny(response.Block, block)
			noError(err, "should have been able to unmarshal received block payload")

			cursor = block.AsRef()
			if traceEnabled {
				zlog.Debug("Block received", zap.Stringer("block", cursor), zap.Stringer("previous", bstream.NewBlockRefFromID(block.PreviousID())))
			}

			stats.recordBlock(int64(response.XXX_Size()))

			now := time.Now()
			if now.After(nextStatus) {
				zlog.Info("Stream blocks progress", zap.Object("stats", stats))
				nextStatus = now.Add(statusFrequency)
			}
		}

		time.Sleep(5 * time.Second)
		stats.restartCount.IncBy(1)
	}

	elapsed := stats.duration()

	fmt.Println("")
	fmt.Println("Completed streaming")
	fmt.Printf("Duration: %s\n", elapsed)
	fmt.Printf("Time to first block: %s\n", stats.timeToFirstBlock)
	if stats.restartCount.total > 0 {
		fmt.Printf("Restart count: %s\n", stats.restartCount.Overall(elapsed))
	}

	fmt.Println("")
	fmt.Printf("Block received: %s\n", stats.blockReceived.Overall(elapsed))
	fmt.Printf("Bytes received: %s\n", stats.bytesReceived.Overall(elapsed))
}

type stats struct {
	startTime        time.Time
	timeToFirstBlock time.Duration
	blockReceived    *counter
	bytesReceived    *counter
	restartCount     *counter
}

func newStats() *stats {
	return &stats{
		startTime:     time.Now(),
		blockReceived: &counter{0, ratecounter.NewRateCounter(1 * time.Second), "block", "s"},
		bytesReceived: &counter{0, ratecounter.NewRateCounter(1 * time.Second), "byte", "s"},
		restartCount:  &counter{0, ratecounter.NewRateCounter(1 * time.Minute), "restart", "m"},
	}
}

func (s *stats) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("block", s.blockReceived.String())
	encoder.AddString("bytes", s.bytesReceived.String())
	return nil
}

func (s *stats) duration() time.Duration {
	return time.Now().Sub(s.startTime)
}

func (s *stats) recordBlock(payloadSize int64) {
	if s.timeToFirstBlock == 0 {
		s.timeToFirstBlock = time.Now().Sub(s.startTime)
	}

	s.blockReceived.IncBy(1)
	s.bytesReceived.IncBy(payloadSize)
}

func newBlockRange(raw string) (out blockRange) {
	input := strings.ReplaceAll(raw, " ", "")
	parts := strings.Split(input, "-")
	ensure(len(parts) == 2, "<range> input should be of the form <start>-<stop> (spaces accepted), got %q", raw)
	ensure(isUint(parts[0]), "the <range> start value %q is not a valid uint64 value", parts[0])
	ensure(isUint(parts[1]), "the <range> end value %q is not a valid uint64 value", parts[1])

	out.start, _ = strconv.ParseUint(parts[0], 10, 64)
	out.end, _ = strconv.ParseUint(parts[1], 10, 64)
	ensure(out.start < out.end, "the <range> start value %q value comes after end value %q", parts[0], parts[1])
	return
}

func isUint(in string) bool {
	_, err := strconv.ParseUint(in, 10, 64)
	return err == nil
}

func errorUsage(message string, args ...interface{}) string {
	return fmt.Sprintf(message+"\n\n"+usage(), args...)
}

func usage() string {
	return `usage: go run . <endpoint> <filter> <range>

Prints consumption stats connection to a dfuse Firehose endpoint like time
taken to fetch blocks, amount of bytes received, throuput stats, etc.

The <filter> is a valid CEL filter expression for the EOSIO network.

The <range> value must be in the form [<start>-<stop>] like "150 000 000 - 150 010 000"
(spaces are trimmed automatically so it's fine to use them).
`
}

func ensure(condition bool, message string, args ...interface{}) {
	if !condition {
		noError(fmt.Errorf(message, args...), "invalid arguments")
	}
}

func noError(err error, message string, args ...interface{}) {
	if err != nil {
		quit(message+": "+err.Error(), args...)
	}
}

func quit(message string, args ...interface{}) {
	fmt.Printf(message+"\n", args...)
	os.Exit(1)
}

type blockRange struct {
	start uint64
	end   uint64
}

func (b blockRange) String() string {
	return fmt.Sprintf("%d - %d", b.start, b.end)
}

type counter struct {
	total    uint64
	counter  *ratecounter.RateCounter
	unit     string
	timeUnit string
}

func (c *counter) IncBy(value int64) {
	if value <= 0 {
		return
	}

	c.counter.Incr(value)
	c.total += uint64(value)
}

func (c *counter) Total() uint64 {
	return c.total
}

func (c *counter) Rate() int64 {
	return c.counter.Rate()
}

func (c *counter) String() string {
	return fmt.Sprintf("%d %s/%s (%d total)", c.counter.Rate(), c.unit, c.timeUnit, c.total)
}

func (c *counter) Overall(elapsed time.Duration) string {
	rate := float64(c.total)
	if elapsed.Minutes() > 1 {
		rate = rate / elapsed.Minutes()
	}

	return fmt.Sprintf("%d %s/%s (%d %s total)", uint64(rate), c.unit, "min", c.total, c.unit)
}
