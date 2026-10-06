# Managed backup encryption keys

The original `encryption.keys` environment-key mappings remain readable. New
`encryption.providers` mappings support AWS KMS and Vault Transit. Select one
with `key_id`; keep old mappings when rotating to a new ID.

## AWS KMS

```yaml
encryption:
  key_id: production-kms
  providers:
    production-kms:
      type: aws-kms
      key: arn:aws:kms:REGION:ACCOUNT:key/IMMUTABLE_KEY_ID
      region: us-east-1
```

The AWS SDK uses its standard credential chain, including workload roles. Grant
`kms:Encrypt` and `kms:Decrypt` on the key. Prefer an immutable ARN/key ID;
retargeting an alias or changing an existing ID mapping can prevent old backups
from decrypting. Automatic KMS key rotation retains access to old ciphertext.
Only the random per-backup data key is sent to KMS; database payloads remain
encrypted locally. An encryption context binds backup metadata and stream
nonce to the wrapped key. S3 server-side KMS is a separate storage setting.

## Vault Transit

```yaml
encryption:
  key_id: production-vault
  providers:
    production-vault:
      type: vault-transit
      endpoint: https://vault.example.com
      mount: transit
      key: dbvault
      token_env: DBVAULT_VAULT_TOKEN
      # namespace: team-a
```

Create an AES-256-GCM Transit key beforehand. The token needs permission to
encrypt/decrypt that key. DBVault never creates or rotates remote keys itself.
Vault's key version is embedded in its ciphertext; Transit rotation works
without changing the local ID mapping. Do not raise minimum decryption versions
past versions still used by retained backups. Associated data binds metadata
and the stream nonce. Tokens remain environment references; the wrapping key
never leaves Vault. Token renewal/AppRole login is managed by the operator or
Vault Agent; DBVault reads the current token for each operation.

TLS certificate verification uses the operating system trust store. HTTPS is
required except explicit loopback HTTP endpoints for development emulators.
URL credentials/query strings and Vault redirects are rejected. Provider error bodies,
tokens and plaintext keys are not included in returned errors.

## Format and restore

Managed backups use `aes256-gcm-stream-v2` with a bounded wrapped-key header and
the existing sequence-authenticated data frames and terminal marker. Backup
identities and metadata are authenticated. v1 environment-key backups keep their
existing format. A logical delta chain may contain both formats/providers.
Restore selects the provider from each backup's key ID; no fallback to another
provider is attempted. Missing permissions, deleted keys and unreachable key
services prevent decryption before destination mutation.

Keep provider ID mappings stable, retain old environment mappings and keep
remote keys available until all dependent backups expire. `doctor` checks
provider configuration and local credentials only; backup/restore check actual
remote wrapping permissions. KMS and Vault audit the remote requests.

## References

- [AWS KMS encryption context](https://docs.aws.amazon.com/kms/latest/developerguide/encrypt_context.html)
- [Vault Transit API](https://developer.hashicorp.com/vault/api-docs/secret/transit)
