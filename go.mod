module github.com/gastrodon/psyduck-std

go 1.18

require (
	github.com/gastrodon/psyduck v1.0.0
	github.com/zclconf/go-cty v1.12.1
)

require golang.org/x/text v0.5.0 // indirect

replace github.com/gastrodon/psyduck => ../psyduck
