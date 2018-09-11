import _ from 'lodash';
import PropTypes from "prop-types";
import React from 'react';
import ReactRouterPropTypes from 'react-router-prop-types';
import { withContext } from './util/AppContext.jsx';
import { Breadcrumb, Layout } from 'antd';
import { friendlyTitle, isResource, singularResource } from "./util/Utils.js";
import './../../css/breadcrumb-header.css';


const routeToCrumbTitle = {
  "servicemesh": "Service Mesh",
  "overview": "Overview",
  "tap": "Tap",
  "top": "Top"
};

class BreadcrumbHeader extends React.Component {

  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    location: ReactRouterPropTypes.location.isRequired,
    pathPrefix: PropTypes.string.isRequired
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
  }

  processResourceDetailURL(segments) {
    if (segments.length === 4) {
      let splitSegments = _.chunk(segments, 2);
      let resourceNameSegment = splitSegments[1];
      resourceNameSegment[0] = singularResource(resourceNameSegment[0]);
      return _.concat(splitSegments[0], resourceNameSegment.join('/'));
    } else {
      return segments;
    }
  }

  convertURLToBreadcrumbs(location) {
    if (location.length === 0) {
      return [];
    } else {
      let segments = location.split('/').slice(1);
      let finalSegments = this.processResourceDetailURL(segments);

      return _.map(finalSegments, segment => {
        let partialUrl = _.takeWhile(segments, s => {
          return s !== segment;
        });

        if (partialUrl.length !== segments.length) {
          partialUrl.push(segment);
        }

        return {
          link: `/${partialUrl.join("/")}`,
          segment: segment
        };
      });
    }
  }

  segmentToFriendlyTitle(segment, isResourceType) {
    if (isResourceType) {
      return routeToCrumbTitle[segment] || friendlyTitle(segment).plural;
    } else {
      return routeToCrumbTitle[segment] || segment;
    }
  }

  renderBreadcrumbSegment(segment, index) {
    let isMeshResource = isResource(segment);

    if (isMeshResource) {
      if (index === 0) {
        return friendlyTitle(segment).singular;
      }
      return this.segmentToFriendlyTitle(segment, true);
    }
    return this.segmentToFriendlyTitle(segment, false);
  }

  renderBreadcrumbs(breadcrumbs) {
    let PrefixedLink = this.api.PrefixedLink;

    if (breadcrumbs.length === 1) {
      // Check for a single segment so that we can pluralize it.
      let singleBreadcrumb = breadcrumbs[0];

      return (
        <Breadcrumb.Item key={singleBreadcrumb.segment}>
          <PrefixedLink
            to={singleBreadcrumb.link}>
            {this.renderBreadcrumbSegment(singleBreadcrumb.segment)}
          </PrefixedLink>
        </Breadcrumb.Item>
      );
    } else {
      return _.map(breadcrumbs, (pathSegment, index) => {
        return (
          <Breadcrumb.Item key={pathSegment.segment}>
            <PrefixedLink
              to={pathSegment.link}>
              {this.renderBreadcrumbSegment(pathSegment.segment, index)}
            </PrefixedLink>
          </Breadcrumb.Item>
        );
      });
    }
  }

  render() {
    let prefix = this.props.pathPrefix;
    let breadcrumbs = this.convertURLToBreadcrumbs(this.props.location.pathname.replace(prefix, ""));

    return (
      <Layout.Header>
        <Breadcrumb separator=">">
          { this.renderBreadcrumbs(breadcrumbs) }
        </Breadcrumb>
      </Layout.Header>
    );
  }
}

export default withContext(BreadcrumbHeader);
