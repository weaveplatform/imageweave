# Historical Ubuntu 24.04 source settings

This directory preserves the reviewed source settings and public signing key
used by the former OCI cloud-image construction workflow. Maintained builds
use Imageweave's reviewed matrix and Packer templates. This reference is not
an additional build pipeline or evidence that an image is qualified.

`weaveoci source fetch` remains the reusable source-integrity primitive. For
these historical Ubuntu cloud inputs, the pinned signer fingerprint is
`D2EB44626FDDC30B513D5BB71A5D6C4C7DB87C81`. Verification requires both a valid
signature over the checksum file and a matching downloaded image hash.

Guest identity, build-account cleanup and reboot persistence must be checked
on the packed/unpacked candidate; they cannot be inferred from the source
name or a successful format conversion.
