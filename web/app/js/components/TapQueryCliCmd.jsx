import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { tapQueryPropType } from './util/TapUtils.js';

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
      <div className="tap-query">
        {
          _.isEmpty(resource) ? null :
          <React.Fragment>
            <div>Current {_.startCase(this.props.cmdName)} query:</div>
            <code>
              linkerd {this.props.cmdName} {resource}
              { this.renderCliItem("--namespace", namespace) }
              { this.renderCliItem("--to", toResource) }
              { this.renderCliItem("--to-namespace", toNamespace) }
              { this.renderCliItem("--method", method) }
              { this.renderCliItem("--scheme", scheme) }
              { this.renderCliItem("--authority", authority) }
              { this.renderCliItem("--path", path) }
              { this.renderCliItem("--max-rps", maxRps) }
            </code>
          </React.Fragment>
        }
      </div>
    );
  }
}
