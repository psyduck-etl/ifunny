package ifunny

type IFunnyConfig struct {
	BearerToken string `psy:"bearer_token"`
	UserAgent   string `psy:"user_agent"`
	APIRoot     string `psy:"api_root"`

	Feed      string `psy:"feed"`
	StopAfter int    `psy:"stop_after"`
}

func mustConfig(parse func(interface{}) error) *IFunnyConfig {
	config := &IFunnyConfig{
		APIRoot: "https://api.ifunny.mobi/v4",
	}

	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}
