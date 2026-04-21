import PropTypes from 'prop-types';
import React from 'react';
import _isEmpty from 'lodash/isEmpty';
import { grafanaIcon } from './util/SvgWrappers.jsx';

const GrafanaLink = function({ PrefixedLink, name, namespace, resource, grafanaExternalUrl, grafanaPrefix }) {
  let link = '/grafana/d/';

  if (grafanaExternalUrl !== '') {
    let baseUrl = grafanaExternalUrl;
    // strip trailing slash if present, to avoid double slash in final URL
    if (grafanaExternalUrl.charAt(grafanaExternalUrl.length - 1) === '/') {
      baseUrl = grafanaExternalUrl.slice(0, grafanaExternalUrl.length - 1);
    }

    // grafanaPrefix is used for externally hosted Grafana instances, which do not use the /grafana proxy.
    // When dashboards for multiple Linkerd instances are deployed to the same Grafana host,
    // the dashboard UID needs a user-specified prefix to be unique.
    link = `${baseUrl}/d/${grafanaPrefix}`;
  }

  link += `linkerd-${resource}?var-${resource}=${name}`;

  if (!_isEmpty(namespace)) {
    link += `&var-namespace=${namespace}`;
  }

  if (grafanaExternalUrl !== '') {
    return (
      // <a> instead of <PrefixedLink> because <Link> doesn't work with external URL's
      <a
        href={link}
        rel="noreferrer"
        target="_blank">
        &nbsp;&nbsp;
        {grafanaIcon}
      </a>
    );
  } else {
    return (
      <PrefixedLink
        to={link}
        targetBlank>
        &nbsp;&nbsp;
        {grafanaIcon}
      </PrefixedLink>
    );
  }
};

GrafanaLink.propTypes = {
  name: PropTypes.string.isRequired,
  namespace: PropTypes.string,
  PrefixedLink: PropTypes.func.isRequired,
  resource: PropTypes.string.isRequired,
  grafanaExternalUrl: PropTypes.string,
  grafanaPrefix: PropTypes.string,
};

GrafanaLink.defaultProps = {
  namespace: '',
  grafanaExternalUrl: '',
  grafanaPrefix: '',
};

export default GrafanaLink;
