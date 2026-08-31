## herdlord targets update

Update a target's prefixes

```
herdlord targets update <name> [flags]
```

### Options

```
      --attach-prefix string   interactive command prefix; empty defaults to prefix
  -h, --help                   help for update
      --prefix string          command prefix; empty selects local Herdr
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

* [herdlord targets](herdlord_targets.md)	 - Manage configured targets
