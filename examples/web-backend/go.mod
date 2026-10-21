module launch-web-example

go 1.26.0

require (
	licentivo-launch v0.0.0
	licentivo/sdk/go v0.0.0
)

replace licentivo-launch => ../..

replace licentivo/sdk/go => ../vendor/sdk/go
