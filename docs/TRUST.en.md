# Windows TLS trust bundle

[中文](TRUST.zh-CN.md) · [Integration](INTEGRATION.en.md)

## What r2 fixes

The previous capture launcher wrote a single observer CA to ca.pem during initialization. Some TLS clients treat SSL_CERT_FILE as a replacement for system roots, so public-service certificate chains could fail verification while the capture proxy itself remained running.

0.4.9-r2 combines the observer CA with public certificates in Windows ROOT that allow TLS server authentication. It deduplicates by DER SHA-256 and sorts deterministically. CurrentUser and LocalMachine stores are opened read-only. The signing CA, private key and proxy identity in identity.json remain unchanged. The archive contains none of the author's CA certificates or credentials.

## Generation and replacement

System roots are enumerated only during capture initialization, with no new polling service. Identical content is left alone. Changes replace the complete file in the same directory, preserve its existing DACL, and retain a recovery point for the old file. Generation errors do not downgrade an existing bundle to a single CA.

PEM is a snapshot of roots at initialization, not the full Windows chain-policy, revocation-policy or automatic-root-download engine. System-root changes are reflected at the next capture initialization. Certificate verification stays enabled, and the observer CA is not imported into Windows root stores.

The implementation is deployment/collector/native_trust_windows.go, projected onto the original writeNativeHome by deployment/build-overlay.cjs. Building native/ directly still produces the retained baseline; the public build entry produces the fixed watcher.

## Updating an existing installation

1. Retain the working programs, identity.json, the working complete ca.pem, and your configuration.
2. Verify the archive's SHA256SUMS and update the programs belonging to that installation. Preserve its layout and signing identity.
3. Replacing files on disk does not load new code into an existing watcher. Normally restart the watcher at your chosen maintenance time, or wait for the next normal Windows startup if configured to start at login. Closing and reopening Codex alone does not guarantee a watcher restart.
4. Confirm the new watcher has loaded, ca.pem still contains the original observer CA plus current system roots, and the original client reconnects.

Updating the capture proxy requires your applicable maintenance authorization. Public packaging scripts start no programs, install no tasks and change no connection settings.

If reverting to the previous watcher, preserve the working complete trust bundle. Its old initialization can overwrite that bundle again, requiring restoration after initialization. This is a temporary rollback, not the persistent fix.

## Validation and sources

Targeted checks cover chains from both roots, usage filtering, repeated initialization, signing identity and setting preservation, invalid input, and replacement blocked by a reader. Independent Windows/Python enumeration matches the public roots in the bundle that restored a real connection. Certificate counts on other computers depend on their stores.

- [CertOpenStore](https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certopenstore)
- [CertGetEnhancedKeyUsage](https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-certgetenhancedkeyusage)
- [ReplaceFileW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew)
