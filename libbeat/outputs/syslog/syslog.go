package syslog

import (
	"log/syslog"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/outputs"
	"github.com/elastic/beats/libbeat/outputs/codec"
	"github.com/elastic/beats/libbeat/publisher"
)

func init() {
	outputs.RegisterType("syslog", makeSyslogout)
}

type syslogOutput struct {
	beat    beat.Info
	stats   *outputs.Stats
	codec   codec.Codec
	tag     string
	address string
	syslog  *syslog.Writer
}

// New instantiates a new file output instance.
func makeSyslogout(
	beat beat.Info,
	stats *outputs.Stats,
	cfg *common.Config,
) (outputs.Group, error) {
	config := defaultConfig
	if err := cfg.Unpack(&config); err != nil {
		return outputs.Fail(err)
	}

	// disable bulk support in publisher pipeline
	cfg.SetInt("bulk_max_size", -1, -1)

	fo := &syslogOutput{beat: beat, stats: stats}
	if err := fo.init(beat, config); err != nil {
		return outputs.Fail(err)
	}

	return outputs.Success(-1, 0, fo)
}

func (out *syslogOutput) init(beat beat.Info, config config) error {
	var err error

	enc, err := codec.CreateEncoder(beat, config.Codec)
	if err != nil {
		return err
	}

	out.codec = enc
	out.address = config.Address
	out.tag = config.Tag
	return nil
}

// Implement Outputer
func (out *syslogOutput) Close() error {
	return nil
}

func (out *syslogOutput) Publish(
	batch publisher.Batch,
) error {
	defer batch.ACK()

	st := out.stats
	events := batch.Events()
	st.NewBatch(len(events))
	logp.Info("syslog Publish %d events", len(events))

	dropped := 0
	for i := range events {
		if out.syslog == nil {
			sysLog, err := syslog.Dial("tcp", out.address,
				syslog.LOG_WARNING|syslog.LOG_DAEMON, out.tag)
			if err != nil {
				logp.Critical("Connection to %s failed with: %v", out.address, err)
				st.WriteError()
				dropped++
				break
			}
			out.syslog = sysLog
		}

		event := &events[i]

		serializedEvent, err := out.codec.Encode(out.beat.Beat, &event.Content)
		if err != nil {
			if event.Guaranteed() {
				logp.Critical("Failed to serialize the event: %v", err)
			} else {
				logp.Warn("Failed to serialize the event: %v", err)
			}

			dropped++
			continue
		}

		_, err = out.syslog.Write(serializedEvent)
		if err != nil {
			out.syslog = nil
			if event.Guaranteed() {
				logp.Critical("Writing event to %s failed with: %v", out.address, err)
			} else {
				logp.Warn("Writing event to %s failed with: %v", out.address, err)
			}

			dropped++
			continue
		}

		st.WriteBytes(len(serializedEvent) + 1)
	}

	st.Dropped(dropped)
	st.Acked(len(events) - dropped)

	return nil
}
