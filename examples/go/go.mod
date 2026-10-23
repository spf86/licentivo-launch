module launch-example

go 1.26.0

require (
	golang.org/x/term v0.21.0
	licentivo-launch v0.0.0
)

require golang.org/x/sys v0.41.0 // indirect

replace licentivo-launch => ../..
