# Linkerd Security Policy

Security is critical to Linkerd and we take it very seriously. Not only must
Linkerd be secure, it must improve the security of the system around it. To this
end, every aspect of Linkerd's development is done with security in mind.

Linkerd makes use of a variety of tools to ensure software security, including:

* Code review
* Dependency hygiene and supply chain security via
  [dependabot](https://docs.github.com/en/code-security/dependabot)
* [Fuzz testing](https://linkerd.io/2021/05/07/fuzz-testing-for-linkerd/)
* [Third-party security audits](#security-audits)
* And other forms of manual, static, and dynamic checking.

## Reporting a Vulnerability

If you believe you've found a security problem in Linkerd, whether in the
control plane, proxy, or any other component, please file a [GitHub security
advisory on the linkerd2
repo](https://github.com/linkerd/linkerd2/security/advisories). The maintainers
will diagnose the severity of the issue and determine how to address the issue.

## Criticality Policy

In general, critical issues that affect Linkerd's security posture or that
reduce its ability to provide security for users will receive immediate
attention and be fixed as quickly as possible.

Issues that do not affect Linkerd's security posture and that don't reduce its
ability to provide security for users may not be immediately addressed. For
example, CVEs in underlying dependencies that don't actually affect Linkerd may
not be immediately addressed.

## Version Policy

A note on versions: For stable releases, Linkerd follows a modified semantic
versioning scheme of the form `2.<major>.<minor>`. In other words, "2" is a
static prefix, followed by the major version, then the minor.

The Linkerd project will provide security updates for the most recent major
stable version only. In other words, if `2.X.Y` is the most recent major
version, we will provide security updates as part of a `2.X.(Y + 1)` release.

Backports of security updates to earlier stable versions will only be done
at the discretion of the maintainer team.

## Security Audits

The CNCF provides periodic third-party security audits. We publish unredacted
reports in the [audits/](audits/) subdirectory.

## Security Advisories

When vulnerabilities in Linkerd itself are discovered and corrected, we will
issue a security advisory, describing the problem and providing a pointer to the
fix. These will be announced to our
[cncf-linkerd-announce](https://lists.cncf.io/g/cncf-linkerd-announce) mailing
list.

There are some situations where we may delay issuing a security advisory. For
example, when a vulnerability is found during a code audit or when several
issues are likely to be spotted and fixed in the near future, the maintainers
may delay the release of a Security Advisory so that we can issue a single
comprehensive Security Advisory covering multiple vulnerabilities. Communication
with vendors and other distributions shipping the same code may also cause these
delays.
