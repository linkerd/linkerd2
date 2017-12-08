import React from 'react';
import * as d3 from 'd3';
import './../../css/line-graph.css';

const defaultSvgWidth = 238;
const defaultSvgHeight = 72;
const margin = { top: 0, right: 0, bottom: 0, left: 0 };

export default class LineGraph extends React.Component {
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
      .attr("transform", "translate(0," + this.state.height + ")");
    this.yAxis = this.svg.append("g");

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
    let data = this.props.data;
    let ymax = d3.max(data, d => parseFloat(d.value));
    let padding = 0;
    if (this.state.svgHeight) {
      padding = ymax / this.state.svgHeight;
    }
    this.xScale.domain(d3.extent(data, d => parseInt(d.timestamp)));
    this.yScale.domain([0-padding, ymax+padding]);
  }

  initializeScales() {
    this.xScale = d3.scaleLinear().range([0, this.state.width]);
    this.yScale = d3.scaleLinear().range([this.state.height, 0]);

    let x = this.xScale;
    let y = this.yScale;

    // define the line
    this.line = d3.line()
      .x(d => x(d.timestamp))
      .y(d => y(d.value));
  }

  initializeGraph() {
    let lineChart = this.svg.append("path")
      .attr("class", "chart-line line");

    lineChart
      .attr("d", this.line(this.props.data));

    this.updateAxes();
  }

  updateGraph(){
    this.svg.select(".line")
      .transition()
      .duration(450)
      .attr("d", this.line(this.props.data));

    this.updateAxes();
  }

  updateAxes() {
    if(this.props.showAxes) {
      this.xAxis
        .call(d3.axisBottom(this.xScale)); // add x axis labels

      this.yAxis
        .call(d3.axisLeft(this.yScale)); // add y axis labels
    }
  }

  render() {
    return (
      <div className={`line-graph ${this.props.containerClassName}`} />
    );
  }
}
