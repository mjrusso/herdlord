package main

import (
	"log"

	"github.com/mjrusso/herdlord/internal/cli"
	"github.com/mjrusso/herdlord/internal/skill"
	"github.com/mjrusso/herdlord/internal/skillcheck"
)

func main() {
	if err := skillcheck.Validate(skill.Markdown(), cli.NewRootCommand()); err != nil {
		log.Fatal(err)
	}
}
