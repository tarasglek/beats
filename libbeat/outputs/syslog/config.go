package syslog

import (
	"github.com/elastic/beats/libbeat/outputs/codec"
)

type config struct {
	Address string       `config:"address"`
	Tag     string       `config:"tag"`
	Codec   codec.Config `config:"codec"`
}

var (
	defaultConfig = config{
		Address: "localhost:564",
		Tag:     "filebeat",
	}
)

func (c *config) Validate() error {
	return nil
}
