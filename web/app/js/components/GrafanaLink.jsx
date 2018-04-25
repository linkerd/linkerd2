import React from 'react';

export default class GrafanaLink extends React.Component {
  render() {
    let resourceVariableName = this.props.resource.toLowerCase().replace(" ", "_");
    let dashboardName = this.props.resource.toLowerCase().replace(" ", "-");
    let ownerInfo = this.props.name.split("/");
    let namespace = ownerInfo[0];
    let name = ownerInfo[1];
    return (
      <this.props.conduitLink
        to={`/dashboard/db/conduit-${dashboardName}?var-namespace=${namespace}&var-${resourceVariableName}=${name}`}
        deployment={"grafana"}
        targetBlank={true}>
        {this.props.name}&nbsp;&nbsp;<i className="fa fa-external-link" />
      </this.props.conduitLink>
    );
  }
}
