import _ from 'lodash';
import MetricsTable from './MetricsTable.jsx';
import React from 'react';
import { rowGutter } from './util/Utils.js';
import { Col, Row } from 'antd';

export default class UpstreamDownstreamTables extends React.Component {
  render() {
    let numUpstreams = _.size(this.props.upstreamMetrics);
    let numDownstreams = _.size(this.props.downstreamMetrics);
    return (
      <Row gutter={rowGutter}>
        <Col span={24}>
          {
            numUpstreams === 0 ? null :
              <div className="upstream-downstream-list">
                <div className="border-container border-neutral subsection-header">
                  <div className="border-container-content subsection-header">Inbound</div>
                </div>
                <MetricsTable
                  resource={`upstream_${this.props.resourceType}`}
                  resourceName={this.props.resourceName}
                  metrics={this.props.upstreamMetrics}
                  api={this.props.api} />
              </div>
          }
          {
            numDownstreams === 0 ? null :
              <div className="upstream-downstream-list">
                <div className="border-container border-neutral subsection-header">
                  <div className="border-container-content subsection-header">Outbound</div>
                </div>
                <MetricsTable
                  resource={`downstream_${this.props.resourceType}`}
                  resourceName={this.props.resourceName}
                  metrics={this.props.downstreamMetrics}
                  api={this.props.api} />
              </div>
          }
        </Col>
      </Row>
    );
  }
}
