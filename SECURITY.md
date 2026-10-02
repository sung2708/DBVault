# Security Policy

The DBVault project takes security seriously. This policy describes our supported versions, reporting procedures, and security scope.

---

## 1. Supported Versions

| Version | Supported | Notes |
|---|:---:|---|
| Latest published stable release | Yes | Fixes ship in a new release; see GitHub Releases |
| Older releases and development snapshots | No | Upgrade before requesting maintenance support |

---

## 2. Reporting a Vulnerability

If you believe you have discovered a security vulnerability in DBVault, please **do NOT report it via public GitHub issues, discussions, or pull requests.**

Instead, please report security concerns privately through GitHub:

1. **GitHub Private Vulnerability Reporting:**
   Open a private advisory via [GitHub Security Advisories](https://github.com/sung2708/DBVault/security/advisories/new).

### What to Include in Your Report
To help us triage and resolve the issue quickly, please provide:
- A detailed description of the vulnerability.
- Steps to reproduce, proof-of-concept scripts, or sample configurations.
- Impact assessment (e.g., potential for credential exposure, command injection, or data tampering).
- Any proposed remediations or patches.

---

## 3. Vulnerability Response Timeline

- **Initial Acknowledgment:** Within 48 hours of receipt.
- **Triage & Assessment:** Within 5 business days.
- **Remediation & Patch Release:** Coordinated with the reporter before public disclosure.

---

## 4. Scope and Threat Model Exclusions

The following are considered out of scope for DBVault security vulnerabilities:
- Attacks requiring root / administrative access to the underlying host running DBVault.
- Compromise of operator-trusted native executables or both backup artifacts and their unsigned manifests. SHA-256 verification is mandatory and detects corruption; it does not authenticate attacker-controlled backups.
- Denial-of-service against the target database caused by issuing intensive dump commands during high-traffic hours.
