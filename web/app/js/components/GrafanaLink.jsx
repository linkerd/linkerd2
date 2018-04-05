import grafanaicon from './../../img/grafana_icon.svg';
import React from 'react';

export default class GrafanaLink extends React.Component {
  render() {
    // TODO: pass along namespace information as well, once grafana supports it, part of:
    // https://github.com/runconduit/conduit/issues/420
    let deployment = this.props.name.split("/")[1];
    return (
      <this.props.conduitLink
        to={`/dashboard/db/conduit-deployment?var-deployment=${deployment}`}
        deployment={"grafana"}
        targetBlank={true}>
        <img
          src={grafanaicon}
          width={this.props.size}
          height={this.props.size}
          title={`${deployment} grafana dashboard`}
          alt={`link to ${deployment} grafana dashboard`} />
      </this.props.conduitLink>
    );
  }
}
