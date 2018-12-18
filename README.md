# GRACE CircleCI Builder [![CircleCI](https://circleci.com/gh/GSA/grace-circleci-builder.svg?style=svg)](https://circleci.com/gh/GSA/grace-circleci-builder)

## Use Case

GRACE CircleCI Builder is a command-line tool that is designed to execute against the CircleCI API v1.1, this tool reads from a local json formatted file, an array of repository definitions (similar to a Puppetfile). Then authenticates to CircleCI using the token provided in the environment variable `CIRCLECI_TOKEN`, then executes and waits for a new [project build](https://circleci.com/docs/api/v1-reference/#new-project-build) for each definition in the file.

Each definition is limited to the following properties:

|name|type|required|description|
| --- | --- | --- | --- |
|name|string|true|circleci project name|
|repository|string|true|version control system url to repository|
|branch|string|false|version control system branch to build in repository|
|tag|string|false|version control system tag to build (cannot be used with branch or commit)|
|commit|string|false|version control system commit to build (full commit hash)|

### Example JSON

```
[{
	"name":"grace-circleci-builder",
	"repository":"https://github.com/GSA/grace-circleci-builder",
	"branch":"master",
	"commit":"d8cbe5e2df067ba5a7eba66376911b064b48a4bf"
},
{
	"name":"grace-tftest",
	"repository":"https://github.com/GSA/grace-tftest",
	"tag":"v0.1"
}]
```

The above would execute a project build for the following two projects:

`grace-circleci-builder (master at d8cbe5)`
`grace-tftest (v0.1)`


## Usage instructions

1. Install system dependencies.
    1. [Go](https://golang.org/)
    1. [Dep](https://golang.github.io/dep/docs/installation.html)
    1. [Go Meta Linter](https://github.com/alecthomas/gometalinter)
    1. [gosec](https://github.com/securego/gosec)
1. Add environment variable `CIRCLECI_TOKEN` with an appropriate value from CircleCI, after creating a [CircleCI API Token](https://circleci.com/docs/2.0/managing-api-tokens/).


## Public domain

This project is in the worldwide [public domain](LICENSE.md). As stated in [CONTRIBUTING](CONTRIBUTING.md):

> This project is in the public domain within the United States, and copyright and related rights in the work worldwide are waived through the [CC0 1.0 Universal public domain dedication](https://creativecommons.org/publicdomain/zero/1.0/).
>
> All contributions to this project will be released under the CC0 dedication. By submitting a pull request, you are agreeing to comply with this waiver of copyright interest.