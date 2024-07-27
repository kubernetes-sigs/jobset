# JobSet Go client example

The code in `main.go` shows a simple example of how you can programmatically create JobSets using
JobSet's `client-go` package.

To run it, simply run the command:

```bash
go run main.go --kubeconfig $KUBE_CONFIG_FILEPATH 
```

You should see the following output:

```
successfully created JobSet: test-js
```