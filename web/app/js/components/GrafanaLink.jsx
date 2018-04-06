import grafanaicon from './../../img/grafana_icon.svg';
import React from 'react';

export default class GrafanaLink extends React.Component {
  render() {
    let ownerInfo = this.props.name.split("/");
    let namespace = ownerInfo[0];
    let deployment = ownerInfo[1];
    return (
      <this.props.conduitLink
        to={`/dashboard/db/conduit-deployment?var-namespace=${namespace}&var-deployment=${deployment}`}
        deployment={"grafana"}
        targetBlank={true}>
        <img
          src={grafanaicon}
          width={this.props.size}
          height={this.props.size}
          title={`${namespace}/${deployment} grafana dashboard`}
          alt={`link to ${namespace}/${deployment} grafana dashboard`} />
      </this.props.conduitLink>
    );
  }
}
