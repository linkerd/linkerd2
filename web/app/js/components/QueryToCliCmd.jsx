import CardContent from '@material-ui/core/CardContent';
import PropTypes from 'prop-types';
import React from 'react';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import _isEmpty from 'lodash/isEmpty';
import _startCase from 'lodash/startCase';
import { displayOrder } from './util/CliQueryUtils.js';
import { withContext } from './util/AppContext.jsx';

const toCliParam = {
  namespace: '--namespace',
  toResource: '--to',
  toNamespace: '--to-namespace',
  method: '--method',
  path: '--path',
  scheme: '--scheme',
  authority: '--authority',
  maxRps: '--max-rps',
  from: '--from',
  from_namespace: '--from-namespace',
};

/*
  prints a given linkerd api query in an equivalent CLI format, such that it
  could be pasted into a terminal
*/
class QueryToCliCmd extends React.Component {
  renderCliItem = (queryLabel, queryVal) => {
    return _isEmpty(queryVal) ? null : ` ${queryLabel} ${queryVal}`;
  }

  render = () => {
    const { cmdName, query, resource, controllerNamespace } = this.props;

    const cmdNameDisplay = _startCase(cmdName);

    return (
      _isEmpty(resource) ? null :
      <CardContent>
        <Typography variant="caption" gutterBottom>
          <Trans>Current {cmdNameDisplay} query</Trans>
        </Typography>

        <br />

        <code>
          linkerd {cmdName} {resource}
          { displayOrder(cmdName, query).map(item => {
            return !toCliParam[item] ? null : this.renderCliItem(toCliParam[item], query[item]);
          })}
          { controllerNamespace === 'linkerd' ? null : ` --linkerd-namespace ${controllerNamespace}`}
        </code>
      </CardContent>
    );
  }
}

QueryToCliCmd.propTypes = {
  cmdName: PropTypes.string.isRequired,
  controllerNamespace: PropTypes.string.isRequired,
  query: PropTypes.shape({}).isRequired,
  resource: PropTypes.string.isRequired,
};

export default withContext(QueryToCliCmd);
