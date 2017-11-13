package syslog

import (
	"fmt"
	"strings"

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
	syslog  *Writer
	codec   codec.Codec
	tag     string
	address string
	network string
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
	out.network = config.Network
	return nil
}

// Implement Outputer
func (out *syslogOutput) Close() error {
	return nil
}

func DeepGetValue(mapStr common.MapStr, key string) (string, error) {
	path := strings.Split(key, ".")
	for i := 0; i < len(path); i++ {
		item := mapStr[path[i]]
		switch v := item.(type) {
		case string:
			if i == len(path)-1 {
				return v, nil
			}
		case common.MapStr:
			mapStr = v
			continue
		}
		break
	}
	return "", fmt.Errorf("Key not found")
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
		var event *publisher.Event = &events[i]
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
		if out.syslog == nil {
			sysLog, err := Dial(out.network, out.address,
				LOG_INFO|LOG_DAEMON, out.tag)
			if err != nil {
				logp.Critical("Connection to %s failed with: %v", out.address, err)
				st.WriteError()
				dropped++
				break
			}
			out.syslog = sysLog
		}
		containerName, err := DeepGetValue(event.Content.Fields, "kubernetes.container.name")
		if err == nil {
			containerNamespace, _ := DeepGetValue(event.Content.Fields, "kubernetes.namespace")
			out.syslog.Hostname = containerName + "." + containerNamespace + ".container"
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
