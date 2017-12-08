import _ from 'lodash';
import { metricToFormatter } from './util/Utils.js';
import React from 'react';
import * as d3 from 'd3';
import './../../css/latency-overview.css';
import './../../css/line-graph.css';

const defaultSvgWidth = 900;
const defaultSvgHeight = 350;
const margin = { top: 20, right: 0, bottom: 30, left: 0 };
const dataDefaults = { P50: [], P95: [], P99: [] };
export default class MultiLineGraph extends React.Component {
  constructor(props) {
    super(props);

    this.state = this.getChartDimensions();
  }

  componentWillMount() {
    this.initializeScales();
  }

  componentDidMount() {
    this.svg = d3.select("." + this.props.containerClassName)
      .append("svg")
        .attr("width", this.state.svgWidth)
        .attr("height", this.state.svgHeight)
      .append("g")
        .attr("transform", "translate(" + this.state.margin.left + "," + this.state.margin.top + ")");

    this.xAxis = this.svg.append("g")
      .attr("class", "x-axis")
      .attr("transform", "translate(0," + this.state.height + ")");

    this.yAxis = this.svg.append("g")
      .attr("class", "y-axis")
      .attr("transform", "translate(" + this.state.width + ",0)");

    this.updateScales();
    this.initializeGraph();
  }

  shouldComponentUpdate(nextProps) {
    if (nextProps.lastUpdated === this.props.lastUpdated) {
      // control whether react re-renders the component
      // only rerender if the input data has changed
      return false;
    }
    return true;
  }

  componentDidUpdate() {
    this.updateScales();
    this.updateGraph();
  }

  getChartDimensions() {
    let svgWidth = this.props.width || defaultSvgWidth;
    let svgHeight = this.props.height || defaultSvgHeight;

    let width = svgWidth - margin.left - margin.right;
    let height = svgHeight - margin.top - margin.bottom;

    return {
      svgWidth: svgWidth,
      svgHeight: svgHeight,
      width: width,
      height: height,
      margin: margin
    };
  }

  updateScales() {
    // update scales over all the latency values
    let data = _.flatten(_.values(this.props.data));

    this.xScale.domain(d3.extent(data, d => parseInt(d.timestamp)));
    this.yScale.domain([0, d3.max(data, d => parseFloat(d.value))]);
  }

  initializeScales() {
    this.xScale = d3.scaleTime().range([0, this.state.width]);
    this.yScale = d3.scaleLinear().range([this.state.height, 0]);

    let x = this.xScale;
    let y = this.yScale;

    // define the line
    this.line = d3.line()
      .x(d => x(d.timestamp))
      .y(d => y(d.value));
  }

  initializeGraph() {
    let d = _.isEmpty(this.props.data) ? dataDefaults : this.props.data;

    this.svg.append("path")
      .attr("class", "chart-line line-p50")
      .attr("d", this.line(d.P50));

    this.svg.append("path")
      .attr("class", "chart-line line-p95")
      .attr("d", this.line(d.P95));

    this.svg.append("path")
      .attr("class", "chart-line line-p99")
      .attr("d", this.line(d.P99));

    this.updateAxes();
  }

  updateGraph(){
    let d = _.isEmpty(this.props.data) ? dataDefaults : this.props.data;

    this.svg.select(".line-p50")
      .transition()
      .duration(450)
      .attr("d", this.line(d.P50));

    this.svg.select(".line-p95")
      .transition()
      .duration(450)
      .attr("d", this.line(d.P95));

      this.svg.select(".line-p99")
      .transition()
      .duration(450)
      .attr("d", this.line(d.P99));

    this.updateAxes();
  }

  updateAxes() {
    // Same as ScatterPlot.jsx
    if(this.props.showAxes) {
      let xAxis = d3.axisBottom(this.xScale)
        .ticks(5)
        .tickSize(5);
      this.xAxis.call(xAxis);

      let yAxis = d3.axisLeft(this.yScale)
        .ticks(4)
        .tickSize(this.state.width)
        .tickFormat(metricToFormatter["LATENCY"]);

      // custom axis styling: https://bl.ocks.org/mbostock/3371592
      let customYAxis = g => {
        g.call(yAxis);
        g.select(".domain").remove();
        g.selectAll(".tick text")
          .attr("x", 4)
          .attr("dx", -10)
          .attr("dy", -4);
      };
      this.yAxis.call(customYAxis);
    }
  }

  renderCurrentLatencies() {
    return (
      <div className="current-latency">
        {
          _.map(["P99", "P95", "P50"], latency => {
            let ts = this.props.data[latency];
            let lat = metricToFormatter["LATENCY"](_.get(_.last(ts), 'value', []));
            return (
              <div key={latency} className={`latency-metric current-latency-${latency}`}>
                <div className="latency-title">Current latency ({latency})</div>
                <div className="latency-value">{lat}</div>
              </div>
            );
          })
        }
      </div>
    );
  }

  render() {
    return (
      <div className={`line-graph ${this.props.containerClassName}`}>
        <div className="subsection-header">Latency Overview</div>
        {this.renderCurrentLatencies()}
      </div>
    );
  }
}
