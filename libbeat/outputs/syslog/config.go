package syslog

import (
	"github.com/elastic/beats/libbeat/outputs/codec"
)

type config struct {
	Address string       `config:"address"`
	Tag     string       `config:"tag"`
	Codec   codec.Config `config:"codec"`
	Network string       `config:"network"`
}

var (
	defaultConfig = config{
		Address: "localhost:564",
		Tag:     "",
		Network: "",
	}
)

func (c *config) Validate() error {
	return nil
}
