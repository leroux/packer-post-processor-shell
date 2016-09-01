package main

import (
	"github.com/mitchellh/packer/packer/plugin"
	"github.com/podpolkovnick/packer-post-processor-shell/shell"
)

func main() {
	server, err := plugin.Server()
	if err != nil {
		panic(err)
	}
	server.RegisterPostProcessor(new(shell.PostProcessor))
	server.Serve()
}
