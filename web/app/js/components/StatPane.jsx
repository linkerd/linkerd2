import _ from 'lodash';
import LatencyOverview from './LatencyOverview.jsx';
import React from 'react';
import { rowGutter } from './util/Utils.js';
import StatPaneStat from './StatPaneStat.jsx';
import { Col, Row } from 'antd';

export default class StatPane extends React.Component {
  render() {
    let latencyTs = _.groupBy(_.get(this.props.summaryMetrics, "LATENCY", []), 'label');
    return (
      <div>
        <Row gutter={rowGutter}>
          <Col span={8}>
            <StatPaneStat
              name="Current request rate"
              metric="REQUEST_RATE"
              lastUpdated={this.props.lastUpdated}
              timeseries={_.get(this.props.summaryMetrics, "REQUEST_RATE", [])} />
          </Col>
          <Col span={8}>
            <StatPaneStat
              name="Current success rate"
              metric="SUCCESS_RATE"
              lastUpdated={this.props.lastUpdated}
              timeseries={_.get(this.props.summaryMetrics, "SUCCESS_RATE", [])} />
          </Col>
        </Row>
        <Row>
          <Col span={24}>
            <div className="latency-chart-container">
              <LatencyOverview
                data={latencyTs}
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
