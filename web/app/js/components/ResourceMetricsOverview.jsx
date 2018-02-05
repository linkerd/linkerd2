import _ from 'lodash';
import LatencyOverview from './LatencyOverview.jsx';
import React from 'react';
import ResourceOverviewMetric from './ResourceOverviewMetric.jsx';
import { rowGutter } from './util/Utils.js';
import { Col, Row } from 'antd';

export default class ResourceMetricsOverview extends React.Component {
  render() {
    return (
      <div>
        <Row gutter={rowGutter}>
          <Col span={8}>
            <ResourceOverviewMetric
              name="Request rate"
              metric="REQUEST_RATE"
              lastUpdated={this.props.lastUpdated}
              window={this.props.window}
              timeseries={_.get(this.props.timeseries, "REQUEST_RATE", [])} />
          </Col>
          <Col span={8}>
            <ResourceOverviewMetric
              name="Success rate"
              metric="SUCCESS_RATE"
              window={this.props.window}
              lastUpdated={this.props.lastUpdated}
              timeseries={_.get(this.props.timeseries, "SUCCESS_RATE", [])} />
          </Col>
        </Row>
        <Row>
          <Col span={24}>
            <div className="latency-chart-container">
              <LatencyOverview
                data={_.get(this.props.timeseries, "LATENCY", {})}
                lastUpdated={this.props.lastUpdated}
                showAxes={true}
                containerClassName="latency-chart-container" />
            </div>
          </Col>
        </Row>
      </div>
    );
  }
}
