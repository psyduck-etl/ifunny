package scyther

type scytherConfig struct {
	URL   string `psy:"url"`
	Queue string `psy:"queue"`
}

func scytherConfigDefault() *scytherConfig {
	return &scytherConfig{}
}
