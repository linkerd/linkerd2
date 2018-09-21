import _ from 'lodash';
import OctopusArms from './util/OctopusArms.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { Col, Progress, Row } from 'antd';
import { displayName, metricToFormatter } from './util/Utils.js';
import { getSuccessRateClassification, srArcClassLabels } from './util/MetricUtils.jsx' ;
import './../../css/octopus.css';

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

  renderResourceSummary(resource, type) {
    let display = displayName(resource);
    return (
      <div key={resource.name} className={`octopus-body ${type}`} title={display}>
        <div className={`octopus-title ${type}-title`}>
          { _.isNil(resource.namespace) ? display :
          <this.props.api.ResourceLink
            resource={resource}
            linkText={display} />
          }
        </div>
        <div>
          <div className="octopus-sr-gauge">
            <Progress
              className={`success-rate-arc ${getSuccessRateClassification(resource.successRate, srArcClassLabels)}`}
              type="dashboard"
              format={() => metricToFormatter["SUCCESS_RATE"](resource.successRate)}
              width={type === "main" ? 132 : 64}
              percent={resource.successRate * 100}
              gapDegree={180} />
          </div>
          <Metric title="RPS" value={metricToFormatter["REQUEST_RATE"](resource.requestRate)} />
          <Metric title="P99" value={metricToFormatter["LATENCY"](_.get(resource, "latency.P99"))} />
        </div>
      </div>
    );
  }

  renderUnmeshedResources = unmeshedResources => {
    return (
      <div key="unmeshed-resources" className="octopus-body neighbor unmeshed">
        <div className="octopus-title neighbor-title">Unmeshed</div>
        { _.map(unmeshedResources, r => <div key={r} title={displayName(r)}>{displayName(r)}</div>) }
      </div>
    );
  }

  renderArrowCol = (numNeighbors, isOutbound) => {
    let baseHeight = 180;
    let width = 75;
    let showArrow = numNeighbors > 0;
    let isEven = numNeighbors % 2 === 0;
    let middleElementIndex = isEven ? ((numNeighbors - 1) / 2) : _.floor(numNeighbors / 2);

    let arrowTypes = _.map(_.times(numNeighbors), i => {
      if (i < middleElementIndex) {
        let height = (_.ceil(middleElementIndex - i) - 1) * baseHeight + (baseHeight / 2);
        return { type: "up", inboundType: "down", height };
      } else if (i === middleElementIndex) {
        return { type: "flat", inboundType: "flat", height: baseHeight };
      } else {
        let height = (_.ceil(i - middleElementIndex) - 1) * baseHeight + (baseHeight / 2);
        return { type: "down", inboundType: "up", height };
      }
    });

    let height = numNeighbors * baseHeight;
    let svg = (
      <svg height={height} width={width} version="1.1" viewBox={`0 0 ${width} ${height}`}>
        <defs />
        {
          _.map(arrowTypes, arrow => {
            let arrowType = isOutbound ? arrow.type : arrow.inboundType;
            return OctopusArms[arrowType](width, height, arrow.height, isOutbound);
          })
        }
      </svg>
    );

    return !showArrow ? null : svg;
  }

  render() {
    let { resource, neighbors, unmeshedSources } = this.props;

    let upstreams = _.sortBy(neighbors.upstream, "resource.name");
    let downstreams = _.sortBy(neighbors.downstream, "resource.name");

    let numUpstreams = _.size(upstreams) + (_.isEmpty(unmeshedSources) ? 0 : 1);
    let hasUpstreams = numUpstreams > 0;
    let hasDownstreams = _.size(neighbors.downstream) > 0;

    return (
      <div className="octopus-graph">
        <Row type="flex" justify="center" align="middle">
          <Col span={6} className={`octopus-col ${hasUpstreams ? "resource-col" : ""}`}>
            {_.map(upstreams, n => this.renderResourceSummary(n, "neighbor"))}
            {_.isEmpty(unmeshedSources) ? null : this.renderUnmeshedResources(unmeshedSources)}
          </Col>

          <Col span={2} className="octopus-col">
            {this.renderArrowCol(numUpstreams, false)}
          </Col>

          <Col span={8} className="octopus-col resource-col">
            {this.renderResourceSummary(resource, "main")}
          </Col>

          <Col span={2} className="octopus-col">
            {this.renderArrowCol(_.size(downstreams), true)}
          </Col>

          <Col span={6} className={`octopus-col ${hasDownstreams ? "resource-col" : ""}`}>
            {_.map(downstreams, n => this.renderResourceSummary(n, "neighbor"))}
          </Col>
        </Row>
      </div>
    );
  }
}
