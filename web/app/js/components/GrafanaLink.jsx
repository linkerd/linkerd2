import PropTypes from 'prop-types';
import React from 'react';
import _isEmpty from 'lodash/isEmpty';
import { grafanaIcon } from './util/SvgWrappers.jsx';

const GrafanaLink = ({ addr, PrefixedLink, redirect, name, namespace, resource }) => {
  let link = 'grafana';

  if (redirect === 'true') {
    link = addr;
  }

  link += `/dashboard/db/linkerd-${resource}?var-${resource}=${name}`;
  if (!_isEmpty(namespace)) {
    link += `&var-namespace=${namespace}`;
  }

  if (redirect === 'true') {
    return (
      <a
        href={link}
        targetBlank>
        &nbsp;&nbsp;
        {grafanaIcon}
      </a>
    );
  }

  return (
    <PrefixedLink
      to={link}
      targetBlank>
      &nbsp;&nbsp;
      {grafanaIcon}
    </PrefixedLink>
  );
};

GrafanaLink.propTypes = {
  addr: PropTypes.string,
  name: PropTypes.string.isRequired,
  namespace: PropTypes.string,
  redirect: PropTypes.string,
  PrefixedLink: PropTypes.func.isRequired,
  resource: PropTypes.string.isRequired,
};

GrafanaLink.defaultProps = {
  addr: '',
  namespace: '',
  redirect: '',
};

export default GrafanaLink;
