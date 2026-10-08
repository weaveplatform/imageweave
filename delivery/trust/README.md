# Public candidate trust

`sigstore-public-good.json` is public verification material, not a private signing
key. It was obtained on 2026-10-08 using GitHub CLI 2.102.0 and
`gh attestation trusted-root`, which validates the instance metadata through TUF.
The command returns JSONL for both the public-good and GitHub signing instances;
OCI's loader accepts **one JSON document**. Only the public-good root was selected.

SHA-256 of the checked-in, formatted JSON:
`6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66`.

Refresh through a reviewed PR when Sigstore rotates trust material. From the
repository root, retrieve and select exactly one authenticated public root:

```sh
gh attestation trusted-root > trusted-root.jsonl
jq -s -e '
  [.[] | select(
    any(.certificateAuthorities[]?; .uri == "https://fulcio.sigstore.dev") and
    any(.tlogs[]?; .baseUrl == "https://rekor.sigstore.dev")
  )] | if length == 1 then .[0] else error("expected one public root") end
' trusted-root.jsonl > delivery/trust/sigstore-public-good.json
sha256sum delivery/trust/sigstore-public-good.json
rm trusted-root.jsonl
```

Review the changed authorities, log keys and validity windows; update the recorded
hash/date and validate with an attestation from the pinned workflow before merge.
Do not fetch or replace roots automatically during candidate verification. Historical
root material can verify historical attestations; successful old verification does
not demonstrate that new signing material is supported.

GitHub's [trusted-root command](https://cli.github.com/manual/gh_attestation_trusted-root)
documents TUF acquisition and the JSONL format. Its
[artifact attestation documentation](https://docs.github.com/en/actions/concepts/security/artifact-attestations)
explains that public repositories use Sigstore Public Good, while private
repositories use GitHub's instance without a public transparency log. The latter
needs a separately designed policy; it is not supported by this public policy.
