# kubectl-dpod

When you have a pod composed of multiple containers, it can be tedious to identify which container
is failing.  `kubectl describe` is just too verbose.  I got tired of searching the describe output for errors
to track down the failed container.

`kubectl-dpod` gives you just enough information about the containers to figure out what is going on
quickly.

## Example

![screenshot](./doc/screenshot.png)

In this example output, you can see that container `msgqueue` is not running, due to an image pull problem.

Container `datagen` is running, but is slower to start up than the other containers.

## Installing

To install, download the appropriate binary from the [release page](https://github.com/jpriebe/kubectl-dpod/releases).  Save it somewhere in your path.

You can also download this repository and install it using Makefile.