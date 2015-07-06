package shell

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/mitchellh/packer/common"
	"github.com/mitchellh/packer/helper/config"
	"github.com/mitchellh/packer/packer"
	"github.com/mitchellh/packer/template/interpolate"
)

type Config struct {
	common.PackerConfig `mapstructure:",squash"`

	// An inline script to execute. Multiple strings are all executed
	// in the context of a single shell.
	Inline []string `mapstructure:"inline"`

	// The shebang value used when running inline scripts.
	InlineShebang string `mapstructure:"inline_shebang"`

	// The local path of the shell script to upload and execute.
	Script string `mapstructure:"script"`

	// An array of environment variables that will be injected before
	// your command(s) are executed.
	Vars []string `mapstructure:"environment_vars"`

	// An array of multiple scripts to run.
	Scripts []string `mapstructure:"scripts"`

	TargetPath string `mapstructure:"target"`

	ctx interpolate.Context
}

type ShellPostProcessor struct {
	cfg Config
}

func (p *ShellPostProcessor) Configure(raws ...interface{}) error {
	err := config.Decode(&p.cfg, &config.DecodeOpts{
		Interpolate: true,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{},
		},
	}, raws...)
	if err != nil {
		return err
	}

	errs := new(packer.MultiError)

	if p.cfg.InlineShebang == "" {
		p.cfg.InlineShebang = "/bin/sh"
	}

	if p.cfg.Scripts == nil {
		p.cfg.Scripts = make([]string, 0)
	}

	if p.cfg.Vars == nil {
		p.cfg.Vars = make([]string, 0)
	}

	if p.cfg.Script != "" && len(p.cfg.Scripts) > 0 {
		errs = packer.MultiErrorAppend(errs,
			errors.New("Only one of script or scripts can be specified."))
	}

	if p.cfg.Script != "" {
		p.cfg.Scripts = []string{p.cfg.Script}
	}

	if p.cfg.TargetPath == "" {
		p.cfg.TargetPath = "packer_{{ .BuildName }}_{{.Provider}}"
	}

	if len(p.cfg.Scripts) == 0 && p.cfg.Inline == nil {
		errs = packer.MultiErrorAppend(errs,
			errors.New("Either a script file or inline script must be specified."))
	} else if len(p.cfg.Scripts) > 0 && p.cfg.Inline != nil {
		errs = packer.MultiErrorAppend(errs,
			errors.New("Only a script file or an inline script can be specified, not both."))
	}

	for _, path := range p.cfg.Scripts {
		if _, err := os.Stat(path); err != nil {
			errs = packer.MultiErrorAppend(errs,
				fmt.Errorf("Bad script '%s': %s", path, err))
		}
	}

	// Do a check for bad environment variables, such as '=foo', 'foobar'
	for _, kv := range p.cfg.Vars {
		vs := strings.SplitN(kv, "=", 2)
		if len(vs) != 2 || vs[0] == "" {
			errs = packer.MultiErrorAppend(errs,
				fmt.Errorf("Environment variable not in format 'key=value': %s", kv))
		}
	}

	if errs != nil && len(errs.Errors) > 0 {
		return errs
	}

	return nil
}

func (p *ShellPostProcessor) PostProcess(ui packer.Ui, artifact packer.Artifact) (packer.Artifact, bool, error) {
	scripts := make([]string, len(p.cfg.Scripts))
	copy(scripts, p.cfg.Scripts)

	if p.cfg.Inline != nil {
		tf, err := ioutil.TempFile("", "packer-shell")
		if err != nil {
			return nil, false, fmt.Errorf("Error preparing shell script: %s", err)
		}
		defer os.Remove(tf.Name())

		// Set the path to the temporary file
		scripts = append(scripts, tf.Name())

		// Write our contents to it
		writer := bufio.NewWriter(tf)
		writer.WriteString(fmt.Sprintf("#!%s\n", p.cfg.InlineShebang))
		for _, command := range p.cfg.Inline {
			if _, err := writer.WriteString(command + "\n"); err != nil {
				return nil, false, fmt.Errorf("Error preparing shell script: %s", err)
			}
		}

		if err := writer.Flush(); err != nil {
			return nil, false, fmt.Errorf("Error preparing shell script: %s", err)
		}

		tf.Close()
	}

	envVars := make([]string, len(p.cfg.Vars)+2)
	envVars[0] = "PACKER_BUILD_NAME=" + p.cfg.PackerBuildName
	envVars[1] = "PACKER_BUILDER_TYPE=" + p.cfg.PackerBuilderType
	copy(envVars[2:], p.cfg.Vars)

	files := artifact.Files()
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	for _, art := range files {
		for _, path := range scripts {
			stderr.Reset()
			stdout.Reset()
			ui.Say(fmt.Sprintf("Process with shell script: %s", path))

			log.Printf("Opening %s for reading", path)
			f, err := os.Open(path)
			if err != nil {
				return nil, false, fmt.Errorf("Error opening shell script: %s", err)
			}
			defer f.Close()

			ui.Message(fmt.Sprintf("Executing script with artifact: %s", art))
			args := []string{path, art}
			cmd := exec.Command("/bin/sh", args...)
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			cmd.Env = envVars
			err = cmd.Run()
			if err != nil {
				return nil, false, fmt.Errorf("Unable to execute script: %s", stderr.String())
			}
			ui.Message(fmt.Sprintf("%s", stderr.String()))
		}
	}
	return artifact, false, nil
}
