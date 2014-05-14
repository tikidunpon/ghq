package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"syscall"

	"github.com/codegangsta/cli"
	"github.com/google/go-github/github"
	"github.com/motemen/ghq/pocket"
	"github.com/motemen/ghq/utils"
)

var Commands = []cli.Command{
	GetCommand,
	ListCommand,
	LookCommand,
	StarredGommand,
	PocketCommand,
}

var GetCommand = cli.Command{
	Name:   "get",
	Usage:  "Clone/sync with a remote repository",
	Action: DoGet,
	Flags: []cli.Flag{
		cli.BoolFlag{"update, u", "Update local repository if cloned already"},
	},
}

var ListCommand = cli.Command{
	Name:   "list",
	Usage:  "List local repositories",
	Action: DoList,
	Flags: []cli.Flag{
		cli.BoolFlag{"exact, e", "Perform an exact match"},
		cli.BoolFlag{"full-path, p", "Print full paths"},
		cli.BoolFlag{"unique", "Print unique subpaths"},
	},
}

var LookCommand = cli.Command{
	Name:   "look",
	Usage:  "Look into a local repository",
	Action: DoLook,
}

var StarredGommand = cli.Command{
	Name:   "starred",
	Usage:  "Get all starred GitHub repositories",
	Action: DoStarred,
	Flags: []cli.Flag{
		cli.BoolFlag{"update, u", "Update local repository if cloned already"},
	},
}

var PocketCommand = cli.Command{
	Name:   "pocket",
	Usage:  "Get all github.com entries in Pocket",
	Action: DoPocket,
	Flags: []cli.Flag{
		cli.BoolFlag{"update, u", "Update local repository if cloned already"},
	},
}

var commandArguments = map[string]string{
	"get":     "[-u] <repository URL> | <user>/<project>",
	"list":    "[-p] [-e] [<query>]",
	"look":    "<project> | <user>/<project> | <host>/<user>/<project>",
	"starred": "[-u] <user>",
	"pocket":  "[-u]",
}

func init() {
	argsTemplate := "{{if false}}"
	for _, command := range Commands {
		args := commandArguments[command.Name]
		argsTemplate = argsTemplate + fmt.Sprintf("{{else if (eq .Name %q)}}%s", command.Name, args)
	}
	argsTemplate = argsTemplate + "{{end}}"

	cli.CommandHelpTemplate = `NAME:
   {{.Name}} - {{.Usage}}

USAGE:
   ghq {{.Name}} ` + argsTemplate + `
{{if (len .Description)}}
DESCRIPTION:
   {{.Description}}
{{end}}{{if (len .Flags)}}
OPTIONS:
   {{range .Flags}}{{.}}
   {{end}}
{{end}}`
}

func DoGet(c *cli.Context) {
	argUrl := c.Args().Get(0)
	doUpdate := c.Bool("update")

	if argUrl == "" {
		cli.ShowCommandHelp(c, "get")
		os.Exit(1)
	}

	url, err := url.Parse(argUrl)
	utils.DieIf(err)

	if !url.IsAbs() {
		url.Scheme = "https"
		url.Host = "github.com"
		if url.Path[0] != '/' {
			url.Path = "/" + url.Path
		}
	}

	remote, err := NewRemoteRepository(url)
	utils.DieIf(err)

	if remote.IsValid() == false {
		utils.Log("error", fmt.Sprintf("Not a valid repository: %s", url))
		os.Exit(1)
	}

	getRemoteRepository(remote, doUpdate)
}

func getRemoteRepository(remote RemoteRepository, doUpdate bool) {
	remoteURL := remote.URL()
	pathParts := append(
		[]string{remoteURL.Host}, strings.Split(remoteURL.Path, "/")...,
	)
	local := LocalRepositoryFromPathParts(pathParts)

	path := local.FullPath
	newPath := false

	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			newPath = true
			err = nil
		}
		utils.PanicIf(err)
	}

	if newPath {
		utils.Log("clone", fmt.Sprintf("%s -> %s", remoteURL, path))
		remote.VCS().Clone(remoteURL, path)
	} else {
		if doUpdate {
			utils.Log("update", path)
			local.VCS().Update(path)
		} else {
			utils.Log("exists", path)
		}
	}
}

func DoList(c *cli.Context) {
	query := c.Args().First()
	exact := c.Bool("exact")
	printFullPaths := c.Bool("full-path")
	printUniquePaths := c.Bool("unique")

	var filterFn func(*LocalRepository) bool
	if query == "" {
		filterFn = func(_ *LocalRepository) bool {
			return true
		}
	} else if exact {
		filterFn = func(repo *LocalRepository) bool {
			return repo.Matches(query)
		}
	} else {
		filterFn = func(repo *LocalRepository) bool {
			return strings.Contains(repo.NonHostPath(), query)
		}
	}

	repos := []*LocalRepository{}

	walkLocalRepositories(func(repo *LocalRepository) {
		if filterFn(repo) == false {
			return
		}

		repos = append(repos, repo)
	})

	if printUniquePaths {
		subpathCount := map[string]int{}

		for _, repo := range repos {
			for _, p := range repo.Subpaths() {
				subpathCount[p] = subpathCount[p] + 1
			}
		}

		for _, repo := range repos {
			for _, p := range repo.Subpaths() {
				if subpathCount[p] == 1 {
					fmt.Println(p)
					break
				}
			}
		}
	} else {
		for _, repo := range repos {
			if printFullPaths {
				fmt.Println(repo.FullPath)
			} else {
				fmt.Println(repo.RelPath)
			}
		}
	}
}

func DoLook(c *cli.Context) {
	name := c.Args().First()

	if name == "" {
		cli.ShowCommandHelp(c, "look")
		os.Exit(1)
	}

	reposFound := []*LocalRepository{}
	walkLocalRepositories(func(repo *LocalRepository) {
		if repo.Matches(name) {
			reposFound = append(reposFound, repo)
		}
	})

	switch len(reposFound) {
	case 0:
		utils.Log("error", "No repository found")

	case 1:
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}

		err := os.Chdir(reposFound[0].FullPath)
		utils.PanicIf(err)

		syscall.Exec(shell, []string{shell}, syscall.Environ())

	default:
		utils.Log("error", "More than one repositories are found; Try more precise name")
		for _, repo := range reposFound {
			utils.Log("error", "- "+strings.Join(repo.PathParts, "/"))
		}
	}
}

func DoStarred(c *cli.Context) {
	user := c.Args().First()

	if user == "" {
		cli.ShowCommandHelp(c, "starred")
		os.Exit(1)
	}

	client := github.NewClient(nil)
	options := &github.ActivityListStarredOptions{Sort: "created"}

	for page := 1; ; page++ {
		options.Page = page

		repositories, res, err := client.Activity.ListStarred(user, options)
		utils.DieIf(err)

		utils.Log("page", fmt.Sprintf("%d/%d", page, res.LastPage))
		for _, repo := range repositories {
			url, err := url.Parse(*repo.HTMLURL)
			if err != nil {
				utils.Log("error", fmt.Sprintf("Could not parse URL <%s>: %s", repo.HTMLURL, err))
				continue
			}

			remote, err := NewRemoteRepository(url)
			if utils.ErrorIf(err) {
				continue
			}

			if remote.IsValid() == false {
				utils.Log("error", fmt.Sprintf("Not a valid repository: %s", url))
				continue
			}

			getRemoteRepository(remote, c.Bool("update"))
		}

		if page == res.LastPage {
			break
		}
	}
}

func DoPocket(c *cli.Context) {
	accessToken, err := GitConfig("ghq.pocket.token")
	utils.PanicIf(err)

	if accessToken == "" {
		receiverURL, ch, err := pocket.StartAccessTokenReceiver()
		utils.PanicIf(err)

		utils.Log("pocket", "Waiting for Pocket authentication callback at "+receiverURL)

		utils.Log("pocket", "Obtaining request token")
		authRequest, err := pocket.ObtainRequestToken(receiverURL)
		utils.DieIf(err)

		url := pocket.GenerateAuthorizationURL(authRequest.Code, receiverURL)
		utils.Log("open", url)

		<-ch

		utils.Log("pocket", "Obtaining access token")
		authorized, err := pocket.ObtainAccessToken(authRequest.Code)
		utils.DieIf(err)

		utils.Log("authorized", authorized.Username)

		accessToken = authorized.AccessToken
		utils.Run("git", "config", "ghq.pocket.token", authorized.AccessToken)
	}

	utils.Log("pocket", "Retrieving github.com entries")
	res, err := pocket.RetrieveGitHubEntries(accessToken)
	utils.DieIf(err)

	for _, item := range res.List {
		url, err := url.Parse(item.ResolvedURL)
		if err != nil {
			utils.Log("error", fmt.Sprintf("Could not parse URL <%s>: %s", item.ResolvedURL, err))
			continue
		}

		remote, err := NewRemoteRepository(url)
		if utils.ErrorIf(err) {
			continue
		}

		if remote.IsValid() == false {
			utils.Log("error", fmt.Sprintf("Not a valid repository: %s", url))
			continue
		}

		getRemoteRepository(remote, c.Bool("update"))
	}
}
