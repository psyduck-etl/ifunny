package scyther

type ScytherConfig struct {
	URL              string `cty:"url"`
	Queue            string `cty:"queue"`
	StopIfExhausted  bool   `cty:"stop-if-exhausted"`
	DelayIfExhausted int    `cty:"delay-if-exhausted"` // time.Duration?
	PerMinute        int    `cty:"per-minute"`
	ExitOnError      bool   `cty:"exit-on-error"`
}

func scytherConfigDefault() *ScytherConfig {
	return &ScytherConfig{}
}

func mustScytherConfig(parse func(interface{}) error) *ScytherConfig {
	config := new(ScytherConfig)
	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}
