# Security Policy

An OSFM instance holds who belongs to a circle of friends, what they recommend and where they go. We take security and privacy reports seriously and appreciate responsible disclosure.

## Reporting a vulnerability

**Please do not report security issues in public GitHub issues, pull requests or discussions.**

Use **GitHub private vulnerability reporting**: open the repository's **Security** tab and click **Report a vulnerability**.

Please include:

- Affected version or commit, and how the instance is deployed
- A description of the issue and its impact (for example: a non-member can read recommendations, a pseudonym can be linked to a real identity, an invitation can be forged)
- Steps to reproduce or a proof of concept
- Whether the issue is already known to anyone else

## What to expect

| Step | Target time |
| --- | --- |
| Acknowledgement of your report | within 5 days |
| Initial assessment and severity rating | within 14 days |
| Fix for critical / high severity issues | as fast as possible, usually within 30 days |
| Public disclosure | coordinated with you, at the latest 90 days after the report |

This is a volunteer project; we will keep you informed and credit you in the advisory unless you prefer to remain anonymous. There is no paid bug bounty.

## Supported versions

Only the latest release (or, before the first release, the `main` branch) receives security fixes.

## Scope

In scope: the code in this repository, the default deployment configuration it ships, and the data export format.

Out of scope: vulnerabilities in third-party services and map tile providers, instances whose operators changed the code, and missing hardening on servers you control.

## Safe harbor

We will not pursue legal action against researchers who act in good faith, avoid privacy violations and data destruction, only test against instances they run themselves or have permission to test, and give us reasonable time to fix the issue before disclosure.
