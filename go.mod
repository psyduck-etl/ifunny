module github.com/gastrodon/psyduck-std

go 1.18

require (
	github.com/gastrodon/psyduck v1.0.0
	github.com/zclconf/go-cty v1.10.0
)

require golang.org/x/text v0.3.7 // indirect

replace github.com/gastrodon/psyduck => ../psyduck
