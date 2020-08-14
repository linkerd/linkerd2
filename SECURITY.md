# Security Policy

## Supported Versions

We provide security updates for the two most recent minor versions released on
the `stable` channel.

For example, if `stable-2.7.1` is the most recent stable versions, we will
address security updates for `stable-2.6.0` and later. Once `stable-2.8.0` is
released, we will no longer provide updates for `stable-2.6.x` releases.

## Reporting a Vulnerability

To report a security problem in Linkerd, please contact the Security Alert Team:
<cncf-linkerd-security-alert@lists.cncf.io>.

The team will help diagnose the severity of the issue and determine how to
address the issue. Issues deemed to be non-critical will be filed as GitHub
issues. Critical issues will receive immediate attention and be fixed as quickly
as possible.

## Security Advisories

When serious security problems in Linkerd are discovered and corrected, we issue
a security advisory, describing the problem and containing a pointer to the fix.
These are announced to our cncf-linkerd-announce mailing list as well as to
various other mailing lists and websites.

Security issues are fixed as soon as possible, and the fixes are propagated to
the stable branches as fast as possible. However, when a vulnerability is found
during a code audit, or when several other issues are likely to be spotted and
fixed in the near future, the security team may delay the release of a Security
Advisory, so that one unique, comprehensive Security Advisory covering several
vulnerabilities can be issued. Communication with vendors and other
distributions shipping the same code may also cause these delays.
