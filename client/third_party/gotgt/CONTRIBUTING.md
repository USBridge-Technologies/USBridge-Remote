# How to contribute #

We'd love to accept your patches and contributions to this project.  There are
a just a few small guidelines you need to follow.

## Submitting a patch ##

  1. It's generally best to start by opening a new issue describing the bug or
     feature you're intending to fix.  Even if you think it's relatively minor,
     it's helpful to know what people are working on.  Mention in the initial
     issue that you are planning to work on that bug or feature so that it can
     be assigned to you.

  1. Follow the normal process of [forking][] the project, and setup a new
     branch to work in.  It's important that each group of changes be done in
     separate branches in order to ensure that a pull request only includes the
     commits related to that bug or feature.

  1. Do your best to have [well-formed commit messages][] for each change.
     This provides consistency throughout the project, and ensures that commit
     messages are able to be formatted properly by various git tools.

  1. Finally, push the commits to your fork and submit a [pull request][].

[forking]: https://help.github.com/articles/fork-a-repo
[well-formed commit messages]: http://tbaggery.com/2008/04/19/a-note-about-git-commit-messages.html
[pull request]: https://help.github.com/articles/creating-a-pull-request

## Coding Style ##

* Go code must be run through 'go fmt'.
* Follow http://golang.org/doc/effective_go.html as much as possible.
    + In particular, http://golang.org/doc/effective_go.html#mixed-caps.  Enums
      should be be CamelCase, with acronyms capitalized (TCPSourcePort, vs.
      TcpSourcePort or TCP_SOURCE_PORT).
* Bonus points for giving enum types a String() field.
* Any exported types or functions should have commentary
   (http://golang.org/doc/effective_go.html#commentary)
