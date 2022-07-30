package scyther

type ScytherConfig struct {
	URL              string `cty:"url"`
	Queue            string `cty:"queue"`
	StopIfExhausted  bool   `cty:"stop-if-exhausted"`
	DelayIfExhausted int    `cty:"delay-if-exhausted"` // time.Duration?
	PerMinute        int    `cty:"per-minute"`
}

func scytherConfigDefault() *ScytherConfig {
	return &ScytherConfig{}
}

func mustScytherConfig(parse func(interface{}) error) *ScytherConfig {
	config := &ScytherConfig{
		StopIfExhausted:  false,
		DelayIfExhausted: 500,
		PerMinute:        0,
	}

	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}
