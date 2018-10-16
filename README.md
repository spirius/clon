**clon** (**cl**oudformati**on**) is a AWS CloudFormation template and stack management tool.

[![Go Report Card](https://goreportcard.com/badge/github.com/spirius/clon)](https://goreportcard.com/report/github.com/spirius/clon)
[![Build Status](https://travis-ci.com/spirius/clon.svg?branch=master)](https://travis-ci.com/spirius/clon)

# Table of conent

- [Overview](#overview)
- [Concepts](#concepts)
  * [Config](#config)
  * [Bootstrap Stack](#bootstrap-stack)
  * [Files](#files)
  * [Variables](#variables)
  * [Template Rendering](#template-rendering)
  * [Strong and week Dependencies](#strong-and-week-dependencies)
- [Installing](#installing)
- [Usage](#usage)
- [Examples](#examples)

# Overview
clon is a AWS CloudFormation stack management and deployment tool. Multiple stacks and cross-dependencies can be managed from single place.

# Concepts

## Config
The list of stacks and their dependencies are defined in config file (default: config.yml).

The configuration syntax is following

**Top level config options**

`Name` - **required** - _(string)_ <br>
Name of the deployment. This value is used as a prefix for all stack names.

`AccountID` -  _(string)_ <br>
clon will make sure that current AWS account is matching to `AccountID`.

`Bootstrap` - **required** - _(Stack)_ <br>
The bootstrap stack configuration.

`Files` - _(map[string]File)_ <br>
Map of files to upload. After files are uploaded (or syned), the information about file is exposed to template rendering.


`Stacks` - _(list[Stack])_ <br>
List of stacks managed by clon.

`Variables` - _(map[string]string)_ <br>
Map of variables. Varables are available in template rendering.


**Stack**

* `Name` - **required** <br>
  The name of the stack.
* `Capabilities` - _(list[string])_ <br>
  List of stack capabilities. Allowed values are `CAPABILITY_IAM` and `CAPABILITY_NAMED_IAM`
* `Template` - **required** - _(String)_ <br>
  Location of template file
* `RoleARN` - _(String)_ <br>
  Location of template file
* `Parameters` - _(map[String]String)_ <br>
  Map of stack parameters
* `Tags` - _(map[String]String)_ <br>
  Map of stack tags

**File**

* `Src` - **required** - _(String)_<br>
  Path of the template file.
* `Bucket` - _(String)_ - 
   _defaults to_: `bootstrap.Outpus.Bucket`<br>
   Destination S3 bucket name.
* `Key` - _(String)_<br>
   _defaults to_: _name of the file_<br>
   S3 bucket key.


## Bootstrap Stack
Bootstrap stack is a special stack, which is used to prepare AWS environment for cloudformation deployment.
This template usually includes some S3 buckets for intermediate file storage and IAM roles and policies for cloudformation stacks.

This stack **must** contain `Bucket` output, which holds the name of that bucket for temporary storage.

### Example of Bootstrap
[![asciicast](https://asciinema.org/a/H7xdtZRFvRSV6XQMjk21fN9TY.png?cols=400)](https://asciinema.org/a/H7xdtZRFvRSV6XQMjk21fN9TY?cols=400 | width=400)

## Files
Files are synced to Dst S3 buckets and location information is available in templates. Files are exposed to template as following structure: 

```yaml
File:
  $MapKey:
    Bucket:       # Name of the bucket
    Key:          # Key of the file in bucket
    VersionID:    # Version ID of file
    Hash:         # MD5 hash of file
    ContentType:  # Content-type of file (optional)
    Region:       # Region of the bucket
    URL:          # URL to file. Can be used for nested-stacks.
```

### Example of Files

## Variables
Variables is simple map[string]string structure. They are exposed to templates as following structures:

```yaml
Var:
  $MapKey: $Value
```

## Template Rendering
`RoleARN`, `Parameters` and `Tags` attributes of stack configuration are rendered using [golang templating](https://golang.org/pkg/text/template/#hdr-Actions) with [sprig](http://masterminds.github.io/sprig/) support.

clon also adds following functions to rendering engine

**file** - read content of file.

Example: `{{ file "path.txt" }}`

**stack** - get stack data. Note, that target stack must be deployed before stack data can be used.

Example:
`{{ (stack "bootstrap").Outputs.Bucket }}`


## Strong and week Dependencies
There are many ways of creating dependency between two stacks, but overall they can be categorized as strong and week dependencies.

### Strong
Strong dependencies are Nested stack dependencies or dependencies created by `Export` output attribute.

**Nested Stacks**

Nested stack dependencies are easy to manage, because CloudFormation will take care for update propagation. But they don't support planning, so it's impossible to identify exactly which resources in nested stacks will be affected.

**Export**

Exported outpus can be imported by other stacks. This means, that those can be trated as separate stacks and change plan can be built. But exported outpus cannot be modified, until there is any dependent stack exists. So, in order to update exported output, one should first remove all dependencies, update relevant resources and create dependencies again. This process includes many manual steps and not easy to automate.

### Weak dependencies
In order to laverage from both features, change planning and automatic updates (if possible), weak reference can be used. The idea behind, is to store the output of one stack in some intermediate storage (like S3 bucket or directly via clon) and update the dependent stack separately with new value.

Note, that this kind of dependency can be created only if dependent resource will not be affected by temporary outdated value.

### Example of weak dependency with clon


# Installation

Get it installed with golang

```
go get github.com/spirius/clon/cmd/clon
```

Or download from [releases](https://github.com/spirius/clon/releases/latest) page.

# Usage

```
clon is a CLoudFormatiON stack management tool

Usage:
  clon [command]

Available Commands:
  deploy      Deploy stack
  destroy     Destroy stack
  execute     Execute previously planned change
  help        Help about any command
  init        Initialize bootstrap stack
  list        List stacks
  plan        Plan stack changes
  status      Show stack status
  version     show version information

Flags:
  -c, --config string            Config file (default "config.yml")
  -e, --config-override string   Override config file
  -d, --debug                    Enable debug mode
  -h, --help                     help for clon
  -i, --input                    User input availability. If not specified, value is identified from terminal. (default true)
  -t, --trace                    Enable error tracing output

Use "clon [command] --help" for more information about a command.
```

