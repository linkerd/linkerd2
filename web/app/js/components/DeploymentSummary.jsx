import LineGraph from './LineGraph.jsx';
import React from 'react';
import { metricToFormatter, toClassName } from './util/Utils.js';

export default class DeploymentSummary extends React.Component {
  title() {
    if (this.props.noLink) {
      return this.props.data.name;
    } else {
      return (<this.props.api.ConduitLink
        to={`/deployment?deploy=${this.props.data.name}`}>
        {this.props.data.name}
      </this.props.api.ConduitLink>);
    }
  }

  render() {
    return (
      <div className={`border-container border-neutral`}>
        <div className="border-container-content">
          <div className="summary-title">{this.title()}</div>
          <div className="summary-info">RPS (last {this.props.api.getMetricsWindowDisplayText()})</div>

          <LineGraph
            data={this.props.requestTs}
            lastUpdated={this.props.lastUpdated}
            containerClassName={toClassName(this.props.data.name)}
            flashLastDatapoint={true} />

          <div className="summary-stat">
            <div className="metric-title">Request rate</div>
            <div className="metric-value">{metricToFormatter["REQUEST_RATE"](this.props.data.requestRate)}</div>
          </div>

          <div className="summary-stat">
            <div className="metric-title">Success rate</div>
            <div className="metric-value">{metricToFormatter["SUCCESS_RATE"](this.props.data.successRate)}</div>
          </div>
        </div>
      </div>
    );
  }
}
