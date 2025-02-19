package main

import (
	"github.com/labstack/echo/v4"
	"github.com/swaggo/echo-swagger"

	_ "github.com/rumsystem/quorum/docs" // docs is generated by Swag CLI, you have to import it.
)

func main() {
	e := echo.New()

	e.GET("/*", echoSwagger.WrapHandler)

	e.Logger.Fatal(e.Start(":1323"))
}