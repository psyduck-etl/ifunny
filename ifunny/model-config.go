package ifunny

type IFunnyConfig struct {
	BearerToken string `psy:"bearer-token"`
	UserAgent   string `psy:"user-agent"`
	APIRoot     string `psy:"api-root"`

	Feed      string `psy:"feed"`
	StopAfter int    `psy:"stop-after"`
}

func mustConfig(parse func(interface{}) error) *IFunnyConfig {
	config := new(IFunnyConfig)
	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}
