import React from 'react';

export default class GrafanaLink extends React.Component {
  render() {
    let resourceVariableName = this.props.resource.toLowerCase().replace(" ", "_");
    let dashboardName = this.props.resource.toLowerCase().replace(" ", "-");

    return (
      <this.props.conduitLink
        to={`/dashboard/db/conduit-${dashboardName}?var-namespace=${this.props.namespace}&var-${resourceVariableName}=${this.props.name}`}
        deployment={"grafana"}
        targetBlank={true}>
        {this.props.displayName || this.props.name}&nbsp;&nbsp;<i className="fa fa-external-link" />
      </this.props.conduitLink>
    );
  }
}
