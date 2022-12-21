package main

type ScytherConfig struct {
	URL              string `psy:"url"`
	Queue            string `psy:"queue"`
	StopIfExhausted  bool   `psy:"stop-if-exhausted"`
	DelayIfExhausted int    `psy:"delay-if-exhausted"` // time.Duration?
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
