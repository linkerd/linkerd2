import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { tapQueryPropType } from './util/TapUtils.jsx';
import {
  CardContent,
  Typography
} from '@material-ui/core';

/*
 prints a given tap query in an equivalent CLI format, such that it
 could be pasted into a terminal
*/
export default class TapQueryCliCmd extends React.Component {
  static propTypes = {
    cmdName: PropTypes.string.isRequired,
    query: tapQueryPropType
  }

  static defaultProps = {
    query: {
      resource: "",
      namespace: "",
      toResource: "",
      toNamespace: "",
      method: "",
      path: "",
      scheme: "",
      authority: "",
      maxRps: ""
    }
  }

  renderCliItem = (queryLabel, queryVal) => {
    return _.isEmpty(queryVal) ? null : ` ${queryLabel} ${queryVal}`;
  }

  render = () => {
    let {
      resource,
      namespace,
      toResource,
      toNamespace,
      method,
      path,
      scheme,
      authority,
      maxRps
    } = this.props.query;

    return (
      _.isEmpty(resource) ? null :
      <CardContent className="tap-query">
        <Typography variant="caption" gutterBottom>
          Current {_.startCase(this.props.cmdName)} query
        </Typography>

        <code>
              linkerd {this.props.cmdName} {resource}
          { resource.indexOf("namespace") === 0 ? null : this.renderCliItem("--namespace", namespace) }
          { this.renderCliItem("--to", toResource) }
          {
            _.isEmpty(toResource) || toResource.indexOf("namespace") === 0 ? null :
                this.renderCliItem("--to-namespace", toNamespace)
          }
          { this.renderCliItem("--method", method) }
          { this.renderCliItem("--scheme", scheme) }
          { this.renderCliItem("--authority", authority) }
          { this.renderCliItem("--path", path) }
          { this.renderCliItem("--max-rps", maxRps) }
        </code>
      </CardContent>
    );
  }
}
