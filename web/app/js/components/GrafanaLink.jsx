import PropTypes from 'prop-types';
import React from 'react';
import _isEmpty from 'lodash/isEmpty';
import { grafanaIcon } from './util/SvgWrappers.jsx';

const GrafanaLink = ({ PrefixedLink, name, namespace, resource }) => {
  let link = `/grafana/dashboard/db/linkerd-${resource}?var-${resource}=${name}`;
  if (!_isEmpty(namespace)) {
    link += `&var-namespace=${namespace}`;
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
  name: PropTypes.string.isRequired,
  namespace: PropTypes.string,
  PrefixedLink: PropTypes.func.isRequired,
  resource: PropTypes.string.isRequired,
};

GrafanaLink.defaultProps = {
  namespace: '',
};

export default GrafanaLink;
