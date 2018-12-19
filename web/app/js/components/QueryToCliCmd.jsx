import CardContent from '@material-ui/core/CardContent';
import PropTypes from 'prop-types';
import React from 'react';
import Typography from '@material-ui/core/Typography';
import _ from 'lodash';
import { displayOrder } from './util/CliQueryUtils.js';
import { withContext } from './util/AppContext.jsx';

const toCliParam = {
  "namespace": "--namespace",
  "toResource": "--to",
  "toNamespace": "--to-namespace",
  "method": "--method",
  "path": "--path",
  "scheme": "--scheme",
  "authority": "--authority",
  "maxRps": "--max-rps",
  "from": "--from",
  "from_namespace": "--from-namespace"
};

/*
  prints a given linkerd api query in an equivalent CLI format, such that it
  could be pasted into a terminal
*/
class QueryToCliCmd extends React.Component {
  static propTypes = {
    cmdName: PropTypes.string.isRequired,
    controllerNamespace: PropTypes.string.isRequired,
    query: PropTypes.shape({}).isRequired,
    resource: PropTypes.string.isRequired
  }

  renderCliItem = (queryLabel, queryVal) => {
    return _.isEmpty(queryVal) ? null : ` ${queryLabel} ${queryVal}`;
  }

  render = () => {
    let { cmdName, query, resource, controllerNamespace } = this.props;

    return (
      _.isEmpty(resource) ? null :
      <CardContent>
        <Typography variant="caption" gutterBottom>
          Current {_.startCase(cmdName)} query
        </Typography>

        <code>
          linkerd {this.props.cmdName} {resource}
          { _.map(displayOrder(cmdName, query), item => {
            return !toCliParam[item] ? null : this.renderCliItem(toCliParam[item], query[item]);
          })}
          { controllerNamespace === "linkerd" ? null : ` --linkerd-namespace ${controllerNamespace}`}
        </code>
      </CardContent>
    );
  }
}

export default withContext(QueryToCliCmd);
