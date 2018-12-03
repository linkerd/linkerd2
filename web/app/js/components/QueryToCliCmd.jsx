import {
  CardContent,
  Typography
} from '@material-ui/core';

import PropTypes from 'prop-types';
import React from 'react';
import _ from 'lodash';

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
export default class QueryToCliCmd extends React.Component {
  static propTypes = {
    cmdName: PropTypes.string.isRequired,
    displayOrder: PropTypes.arrayOf(PropTypes.string).isRequired,
    query: PropTypes.shape({}).isRequired,
    resource: PropTypes.string.isRequired
  }

  renderCliItem = (queryLabel, queryVal) => {
    return _.isEmpty(queryVal) ? null : ` ${queryLabel} ${queryVal}`;
  }

  render = () => {
    let { cmdName, query, resource, displayOrder } = this.props;

    return (
      _.isEmpty(resource) ? null :
      <CardContent>
        <Typography variant="caption" gutterBottom>
          Current {_.startCase(cmdName)} query
        </Typography>

        <code>
          linkerd {this.props.cmdName} {resource}
          { _.map(displayOrder, item => {
            return !toCliParam[item] ? null : this.renderCliItem(toCliParam[item], query[item]);
          })}
        </code>
      </CardContent>
    );
  }
}
