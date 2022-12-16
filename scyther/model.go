package scyther

type ScytherConfig struct {
	URL              string `psy:"url"`
	Queue            string `psy:"queue"`
	StopIfExhausted  bool   `psy:"stop-if-exhausted"`
	DelayIfExhausted int    `psy:"delay-if-exhausted"` // time.Duration?
	PerMinute        int    `psy:"per-minute"`
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
