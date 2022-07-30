package ifunny

type IFunnyConfig struct {
	BearerToken string `cty:"bearer-token"`
	UserAgent   string `cty:"user-agent"`
	APIRoot     string `cty:"api-root"`

	Feed      string `cty:"feed"`
	StopAfter int    `cty:"stop-after"`
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
