import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { Col, Icon, Progress, Row } from 'antd';
import { metricToFormatter, toShortResourceName } from './util/Utils.js';
import './../../css/octopus.css';

const displayName = resource => `${toShortResourceName(resource.type)}/${resource.name}`;

const getSrClassification = sr => {
  if (sr < 0.9) {
    return "status-poor";
  } else if (sr < 0.95) {
    return "status-ok";
  } else {return "status-good";}
};

const Metric = ({title, value, className}) => {
  return (
    <Row type="flex" justify="center" className={`octopus-metric ${className}`}>
      <Col span={12} className="octopus-metric-title"><div>{title}</div></Col>
      <Col span={12} className="octopus-metric-value"><div>{value}</div></Col>
    </Row>
  );
};
Metric.defaultProps = { className: "" };
Metric.propTypes = {
  className: PropTypes.string,
  title: PropTypes.string.isRequired,
  value: PropTypes.string.isRequired
};

const ArrowCol = ({showArrow}) => <Col span={2} className="octopus-col">{!showArrow ? " " : <Icon type="arrow-right" />}</Col>;
ArrowCol.propTypes = PropTypes.bool.isRequired;
export default class Octopus extends React.Component {
  static defaultProps = {
    neighbors: {},
    resource: {},
    unmeshedSources: []
  }

  static propTypes = {
    neighbors: PropTypes.shape({}),
    resource: PropTypes.shape({}),
    unmeshedSources: PropTypes.arrayOf(PropTypes.shape({})),
  }

  renderResourceSummary(resource, type, unmeshed) {
    return (
      <div key={resource.name} className={`octopus-body ${type}`}>
        <div className={`octopus-title ${type}`}>
          <this.props.api.ResourceLink
            resource={resource}
            linkText={displayName(resource)} />
        </div>
        {
          unmeshed ? <div>Unmeshed</div> :
          <div>
            <div className="octopus-sr-gauge">
              <Progress
                className={getSrClassification(resource.successRate)}
                type="dashboard"
                format={() => metricToFormatter["SUCCESS_RATE"](resource.successRate)}
                width={type === "main" ? 132 : 64}
                percent={resource.successRate * 100}
                gapDegree={180} />
            </div>
            <Metric title="RPS" value={metricToFormatter["REQUEST_RATE"](resource.requestRate)} />
            <Metric title="P99" value={metricToFormatter["LATENCY"](_.get(resource, "latency.P99"))} />
          </div>
        }
      </div>
    );
  }

  render() {
    let { resource, neighbors, unmeshedSources } = this.props;
    let hasUpstreams = _.size(neighbors.upstream) > 0 || _.size(unmeshedSources) > 0;
    let hasDownstreams = _.size(neighbors.downstream) > 0;

    return (
      <div className="octopus-graph">
        <Row type="flex" justify="center" gutter={32} align="middle">
          <Col span={6} className={`octopus-col ${hasUpstreams ? "resource-col" : ""}`}>
            {_.map(_.sortBy(neighbors.upstream, "resource.name"), n => this.renderResourceSummary(n, "neighbor"))}
            {_.map(_.sortBy(unmeshedSources, "name"), n => this.renderResourceSummary(n, "neighbor", true))}
          </Col>

          <ArrowCol showArrow={hasUpstreams} />

          <Col span={8} className="octopus-col resource-col">
            {this.renderResourceSummary(resource, "main")}
          </Col>

          <ArrowCol showArrow={hasDownstreams} />

          <Col span={6} className={`octopus-col ${hasDownstreams ? "resource-col" : ""}`}>
            {_.map(_.sortBy(neighbors.downstream, "resource.name"), n => this.renderResourceSummary(n, "neighbor"))}
          </Col>
        </Row>
      </div>
    );
  }
}
