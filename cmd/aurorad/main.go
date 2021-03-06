package main

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/globalsign/mgo"
	"github.com/globalsign/mgo/bson"
	"github.com/kovetskiy/aur-go"
	"github.com/kovetskiy/aurora/pkg/proto"
	"github.com/kovetskiy/lorg"
)

var (
	version = "[manual build]"
	usage   = "aurorad " + version + `

Usage:
  aurorad [options] -L
  aurorad [options] -A <package>... [-p <priority>]
  aurorad [options] -R <package>...
  aurorad [options] -Q
  aurorad [options] -P
  aurorad [options] --generate-config
  aurorad -h | --help
  aurorad --version

Options:
  -L --listen         Listen specified address [default: :80].
  -A --add            Add specified package to watch and make cycle queue.
  -R --remove         Remove specified package from watch and make cycle queue.
  -P --process        Process watch and make cycle queue.
  -Q --query          Query package database.
  -c --config <path>  Configuration file path.
                       [default: ` + defaultConfigPath + `]
  -p --priority <n>   Priority level of the package [default: 0].
  -h --help           Show this screen.
  --version           Show version.
`
)

var logger = lorg.NewLog()

func bootstrap(args map[string]interface{}) {
	logger.SetFormat(
		lorg.NewFormat(
			"${time} ${level:[%s]:right:short} ${prefix}%s",
		),
	)

	logger.SetIndentLines(true)

	aur.SetLogger(logger)
}

func main() {
	args, err := docopt.Parse(usage, nil, true, version, false)
	if err != nil {
		panic(err)
	}

	bootstrap(args)

	infof("starting up aurorad %s", version)

	if args["--generate-config"].(bool) {
		err := GenerateConfig(args["--config"].(string))
		if err != nil {
			fatalh(
				err,
				"unable to generate default config at %s",
				args["--config"].(string),
			)
		}

		os.Exit(0)
	}

	config, err := GetConfig(args["--config"].(string))
	if err != nil {
		fatalh(err, "unable to load config")
	}

	if config.Debug {
		logger.SetLevel(lorg.LevelDebug)
	}

	if config.Trace {
		logger.SetLevel(lorg.LevelTrace)
	}

	database, err := NewDatabase("mongodb://localhost/aurora")
	if err != nil {
		fatalh(err, "can't open aurora database")
	}

	packages := database.C("packages")

	err = packages.EnsureIndex(mgo.Index{
		Key:    []string{"name"},
		Unique: true,
	})
	if err != nil {
		fatalh(err, "can't ensure index for collection")
	}

	switch {
	case args["--add"].(bool):
		priority, _ := strconv.Atoi(args["--priority"].(string))
		err = addPackage(packages, args["<package>"].([]string), priority)

	case args["--remove"].(bool):
		err = removePackage(packages, args["<package>"].([]string))

	case args["--process"].(bool):
		err = processQueue(packages, config)

	case args["--query"].(bool):
		err = queryPackage(packages)

	case args["--listen"].(bool):
		err = serveWeb(packages, config)
	}

	if err != nil {
		fatalln(err)
	}
}

func addPackage(collection *mgo.Collection, packages []string, priority int) error {
	var err error

	for _, name := range packages {
		err = collection.Insert(
			proto.Package{
				Name:     name,
				Status:   proto.BuildStatusQueued.String(),
				Date:     time.Now(),
				Priority: priority,
			},
		)

		if err == nil {
			infof("package %s has been added", name)
		} else if mgo.IsDup(err) {
			warningf("package %s has not been added: already exists", name)
		} else {
			return err
		}
	}

	return nil
}

func removePackage(collection *mgo.Collection, packages []string) error {
	var err error

	for _, name := range packages {
		err = collection.Remove(
			bson.M{"name": name},
		)

		if err == nil {
			infof("package %s has been removed", name)
		} else if err == mgo.ErrNotFound {
			warningf("package %s not found", name)
		} else {
			return err
		}
	}

	return nil
}

func queryPackage(collection *mgo.Collection) error {
	var (
		pkg      = proto.Package{}
		packages = collection.Find(bson.M{}).Iter()
		table    = tabwriter.NewWriter(os.Stdout, 1, 4, 1, ' ', 0)
	)

	for packages.Next(&pkg) {
		fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\n",
			pkg.Name, pkg.Version, pkg.Status,
			pkg.Date.Format("2006-01-02 15:04:05"),
		)
	}

	return table.Flush()
}
