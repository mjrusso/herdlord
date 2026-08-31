## herdlord read

Print recent output from one agent pane

```
herdlord read <target> <pane> [flags]
```

### Options

```
  -h, --help        help for read
      --lines int   number of recent lines (default 50)
```

### Options inherited from parent commands

```
      --config string       targets file (default: user config directory)
      --format string       output format: table or json
      --interval duration   TUI poll interval (default 2s)
      --output string       output format: text or json (default "text")
      --timeout duration    timeout for each Herdr command (default 10s)
```

### SEE ALSO

* [herdlord](herdlord.md)	 - See every Herdr agent in one place
