import _ from 'lodash';
import Metric from './Metric.jsx';
import { metricToFormatter } from './util/Utils.js';
import React from 'react';
import { Col, Progress, Row } from 'antd';
import './../../css/health-pane.css';

const TrafficIndicator = () => {
  return (
    <Progress
      percent={100}
      status="active"
      strokeWidth={8}
      format={() => null} />
  );
};

const neutralSr = 0.5;

export default class HealthPane extends React.Component {
  getRequestRate(metrics) {
    return _.sumBy(metrics, 'requestRate');
  }

  getAvgSuccessRate(metrics) {
    return _.meanBy(metrics, 'successRate');
  }

  getHealthClassName(successRate) {
    if (successRate < 0.4) {
      return "health-bad";
    }
    if (successRate > 0.95) {
      return "health-good";
    }
    return "health-neutral";
  }

  getHealthStats() {
    let inboundSr = this.getAvgSuccessRate(this.props.upstreamMetrics);
    let outboundSr = this.getAvgSuccessRate(this.props.downstreamMetrics);
    let sr = _.isUndefined(this.props.currentSr) ? neutralSr : this.props.currentSr;

    return {
      inbound: {
        requests: metricToFormatter["REQUEST_RATE"](this.getRequestRate(this.props.upstreamMetrics)),
        health: this.getHealthClassName(inboundSr)
      },
      outbound: {
        requests: metricToFormatter["REQUEST_RATE"](this.getRequestRate(this.props.downstreamMetrics)),
        health: this.getHealthClassName(outboundSr)
      },
      current: {
        health: this.getHealthClassName(sr)
      }
    };
  }

  render() {
    let stats = this.getHealthStats();

    return (
      <div key="entity-heath" className="entity-health">
        <div className="subsection-header">{this.props.entityType} Health</div>
        <Row>
          <Col span={8}>
            <Metric title="Inbound request rate" value={stats.inbound.requests} className="float-right" />
          </Col>
          <Col span={8} />
          <Col span={8}>
            <Metric title="Outbound request rate" value={stats.outbound.requests} className="float-left" />
          </Col>
        </Row>

        <Row>
          <Col span={8}>
            <div className="entity-count">&laquo; {_.size(this.props.upstreamMetrics)} {this.props.entityType}s</div>
            <div className={`adjacent-health ${stats.inbound.health}`}>
              <TrafficIndicator />
            </div>
          </Col>
          <Col span={8}>
            <div className="entity-count">&nbsp;</div>
            <div className={`entity-title ${stats.current.health}`}>{this.props.entity}</div>
          </Col>
          <Col span={8}>
            <div className="entity-count float-right">{_.size(this.props.downstreamMetrics)} {this.props.entityType}s &raquo;</div>
            <div className={`adjacent-health ${stats.outbound.health}`}>
              <TrafficIndicator />
            </div>
          </Col>
        </Row>
      </div>
    );
  }
}
