# sample-data

The **default catalogue** is embedded in the binary from
[`internal/catalogue/default.yaml`](../internal/catalogue/default.yaml) (synthetic entries only).
To serve a different catalogue, point the daemon at a file:

```sh
MCP_HUB_CATALOGUE=/path/to/catalogue.yaml mcp-hub serve
```

The file is parsed strictly (unknown fields are errors) and validated before the daemon starts;
an invalid catalogue stops startup with every problem listed (entry id + field).

- [`invalid-catalogue.yaml`](invalid-catalogue.yaml) — intentionally invalid, to demo the
  validator. `TestSampleInvalidCatalogueIsRejected` keeps it invalid.
