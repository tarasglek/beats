// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

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
	beat     beat.Info
	observer outputs.Observer
	syslog   *Writer
	codec    codec.Codec
	tag      string
	address  string
	network  string
}

// New instantiates a new file output instance.
func makeSyslogout(
	beat beat.Info,
	observer outputs.Observer,
	cfg *common.Config,
) (outputs.Group, error) {
	config := defaultConfig
	if err := cfg.Unpack(&config); err != nil {
		return outputs.Fail(err)
	}

	// disable bulk support in publisher pipeline
	cfg.SetInt("bulk_max_size", -1, -1)

	fo := &syslogOutput{beat: beat, observer: observer}
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

func getStringMember(mapStr common.MapStr, keyPath string) (string, error) {
	maybeStr, _ := mapStr.GetValue(keyPath)
	myStr, ok := maybeStr.(string)
	if ok {
		return myStr, nil
	}
	return "", fmt.Errorf("Not a member string")
}

func (out *syslogOutput) Publish(
	batch publisher.Batch,
) error {
	defer batch.ACK()

	st := out.observer
	events := batch.Events()
	st.NewBatch(len(events))

	dropped := 0
	for i := range events {
		if out.syslog == nil {
			sysLog, err := Dial(out.network, out.address,
				LOG_INFO|LOG_DAEMON, out.tag)
			if err != nil {
				st.WriteError(err)
				logp.Critical("Connection to %s failed with: %v", out.address, err)
				dropped++
				break
			}
			out.syslog = sysLog
		}
		var virtualHostname string

		event := &events[i]
		fields := &event.Content.Fields

		var flowErr error
		if flowName, err := getStringMember(*fields, "kubernetes.labels.flow"); err == nil {
			virtualHostname = flowName
		} else {
			flowErr = err
		}

		if stepName, err := getStringMember(*fields, "kubernetes.labels.step"); err == nil && flowErr == nil {
			virtualHostname = virtualHostname + "-" + stepName
		} else if podName, err := getStringMember(*fields, "kubernetes.pod.name"); err == nil {
			containerNamespace, _ := getStringMember(*fields, "kubernetes.namespace")
			virtualHostname = containerNamespace + "-" + podName
		} else if sourceFile, err := getStringMember(*fields, "source"); err == nil {
			parts := strings.Split(sourceFile, "/")
			virtualHostname = parts[len(parts)-1]
			if virtualHostname == "syslog" {
				// pull out 4th field(hostname) out of syslog message
				if message, err := getStringMember(*fields, "message"); err == nil {
					for i := 0; i <= 3; i++ {
						spacePos := strings.Index(message, " ")
						if spacePos == -1 {
							break
						} else if i < 3 {
							message = message[spacePos+1:]
						} else {
							virtualHostname = message[:spacePos]
						}
					}
				}
			}
		}

		// mangle docker logs to have a .message
		if hasKey, _ := (*fields).HasKey("message"); !hasKey {
			fields.Put("message", (*fields)["log"])
			fields.Put("docker_timestamp", (*fields)["time"])
			fields.Delete("log")
			fields.Delete("time")
		}

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

		out.syslog.Hostname = virtualHostname
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

func (out *syslogOutput) String() string {
	return "syslog"
}
